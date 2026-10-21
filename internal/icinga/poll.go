package icinga

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"golang.org/x/sync/errgroup"
)

const (
	defaultPollWorkers = 32
	maxPollWorkers     = 256
)

type resultSubmitter interface {
	Submit(ctx context.Context, results []checkResult) error
}

// PollOptions controls one factum2-icinga poll run. Open and Submit are
// set by tests. A nil Open uses SNMPv2c. A nil Submit uses the command
// pipe when CommandPipe is set, otherwise the Icinga API.
type PollOptions struct {
	Workers     int
	Interval    time.Duration
	StateFile   string
	CommandPipe string
	Open        func(ctx context.Context, addr, community string) (snmpQuerier, error)
	Submit      resultSubmitter
}

type pollStats struct {
	Devices    int
	Interfaces int
	SNMPErrors int
	Submitted  int
}

type counterStore struct {
	mu  sync.Mutex
	cur map[string]ifaceCounters
}

func counterKey(host, service string) string {
	return host + "\x00" + service
}

func (s *counterStore) get(host, service string) *ifaceCounters {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cur[counterKey(host, service)]
	if !ok {
		return nil
	}
	cp := c
	return &cp
}

func (s *counterStore) put(host, service string, c ifaceCounters) {
	s.mu.Lock()
	s.cur[counterKey(host, service)] = c
	s.mu.Unlock()
}

func loadCounterStore(path string) (*counterStore, error) {
	s := &counterStore{cur: map[string]ifaceCounters{}}
	if path == "" {
		return s, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if line != "v1" {
				return nil, fmt.Errorf("%s: unexpected counter state header %q", path, line)
			}
			continue
		}
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 9 {
			continue
		}
		at, err1 := strconv.ParseInt(parts[2], 10, 64)
		fields := make([]uint64, 6)
		ok := err1 == nil
		for i := 0; i < 6 && ok; i++ {
			fields[i], err1 = strconv.ParseUint(parts[3+i], 10, 64)
			ok = err1 == nil
		}
		if !ok {
			continue
		}
		s.cur[counterKey(parts[0], parts[1])] = ifaceCounters{
			At: at, InOctets: fields[0], OutOctets: fields[1],
			InErrors: fields[2], OutErrors: fields[3],
			InDiscards: fields[4], OutDiscards: fields[5],
		}
	}
	return s, sc.Err()
}

func (s *counterStore) save(path string) error {
	if path == "" {
		return nil
	}
	s.mu.Lock()
	keys := make([]string, 0, len(s.cur))
	for k := range s.cur {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("v1\n")
	for _, k := range keys {
		host, service, _ := strings.Cut(k, "\x00")
		c := s.cur[k]
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
			host, service, c.At, c.InOctets, c.OutOctets, c.InErrors, c.OutErrors, c.InDiscards, c.OutDiscards)
	}
	s.mu.Unlock()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func dialDevice(ctx context.Context, open func(context.Context, string, string) (snmpQuerier, error), addr string, communities []string) (snmpQuerier, map[int]snmpValue, error) {
	if len(communities) == 0 {
		return nil, nil, errors.New("no SNMP communities configured")
	}
	var last error
	for _, community := range communities {
		q, err := open(ctx, addr, community)
		if err != nil {
			last = err
			continue
		}
		names, err := q.Walk(oidIfName)
		if err != nil {
			q.Close()
			last = err
			continue
		}
		return q, names, nil
	}
	if last == nil {
		last = errors.New("snmp poll failed")
	}
	return nil, nil, last
}

func unknownResults(dev deviceIfaces, text string) []checkResult {
	out := make([]checkResult, len(dev.Ifaces))
	for i, iface := range dev.Ifaces {
		out[i] = checkResult{Host: dev.Host, Service: iface.Service, Status: statusUnknown, Output: iface.Name + " " + text}
	}
	return out
}

func pollDevice(ctx context.Context, open func(context.Context, string, string) (snmpQuerier, error), communities []string, dev deviceIfaces, store *counterStore, now time.Time) ([]checkResult, bool) {
	if err := ctx.Err(); err != nil {
		return unknownResults(dev, "poll cancelled"), true
	}
	q, names, err := dialDevice(ctx, open, dev.Addr, communities)
	if err != nil {
		return unknownResults(dev, "SNMP poll failed: "+err.Error()), true
	}
	defer q.Close()
	samples, err := collectSamples(q, names, dev.Ifaces)
	if err != nil {
		return unknownResults(dev, "SNMP poll failed: "+err.Error()), true
	}
	out := make([]checkResult, len(dev.Ifaces))
	for i, iface := range dev.Ifaces {
		sample, ok := samples[iface.Service]
		if !ok {
			out[i] = checkResult{Host: dev.Host, Service: iface.Service, Status: statusUnknown, Output: iface.Name + " not in SNMP"}
			continue
		}
		prev := store.get(dev.Host, iface.Service)
		status, output, perf, next, keep := evalInterface(iface, sample, prev, now)
		if keep {
			store.put(dev.Host, iface.Service, next)
		}
		out[i] = checkResult{Host: dev.Host, Service: iface.Service, Status: status, Output: output, Perf: perf}
	}
	return out, false
}

func (fic *FactumIcingaClient) pollOnce(ctx context.Context, opt PollOptions, devices []*models.Device, store *counterStore, submit resultSubmitter, now time.Time) (pollStats, error) {
	targets := selectInterfaceTargets(devices, fic.IcingaConfig.DefaultDomain, fic.IcingaConfig.IgnoreDevices)
	stats := pollStats{Devices: len(targets), Interfaces: countIfaces(targets)}
	if stats.Interfaces == 0 {
		return stats, nil
	}
	open := opt.Open
	if open == nil {
		open = openGoSNMP
	}
	communities := splitLines(fic.IcingaConfig.SNMPCommunities)
	workers := opt.Workers
	if workers < 1 {
		workers = defaultPollWorkers
	}
	if workers > maxPollWorkers {
		workers = maxPollWorkers
	}
	var mu sync.Mutex
	var submitErr error
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, dev := range targets {
		g.Go(func() error {
			results, snmpErr := pollDevice(gctx, open, communities, dev, store, now)
			if snmpErr {
				mu.Lock()
				stats.SNMPErrors++
				mu.Unlock()
			}
			if err := submit.Submit(gctx, results); err != nil {
				mu.Lock()
				if submitErr == nil {
					submitErr = err
				}
				mu.Unlock()
				return nil
			}
			mu.Lock()
			stats.Submitted += len(results)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return stats, err
	}
	return stats, submitErr
}

// Poll loads Icinga-monitored devices and submits one passive result per
// interface that has Interface alarms set. Interval greater than zero
// repeats until ctx is cancelled. Counter state is kept in memory and,
// when StateFile is set, on disk so a later cycle can compute bit rates.
func (fic *FactumIcingaClient) Poll(ctx context.Context, opt PollOptions, reporter jobevent.Reporter) error {
	if fic.IcingaConfig == nil || !fic.IcingaConfig.MonitorInterfaces {
		reporter.Emit(jobevent.Info, "Interface monitoring is off")
		return nil
	}
	if len(splitLines(fic.IcingaConfig.SNMPCommunities)) == 0 {
		err := errors.New("no SNMP communities configured")
		reporter.EmitErr(err)
		return err
	}
	submit := opt.Submit
	if submit == nil {
		if opt.CommandPipe != "" {
			submit = &pipeSubmitter{path: opt.CommandPipe}
		} else if fic.Icinga != nil && strings.TrimSpace(fic.Icinga.c.URL) != "" {
			reporter.Emit(jobevent.Warning, "worker.paths.icinga_command_pipe is empty; submitting interface results through the Icinga API does not keep up with tens of thousands of interfaces")
			submit = &apiSubmitter{icinga: fic.Icinga}
		} else {
			err := errors.New("set worker.paths.icinga_command_pipe or the Icinga API URL")
			reporter.EmitErr(err)
			return err
		}
	}
	store, err := loadCounterStore(opt.StateFile)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	factumClient := factum.NewFactumClient(fic.Config)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		started := time.Now()
		devices, err := factumClient.GetDevicesWithInterfaces()
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
		monitored := make([]*models.Device, 0, len(devices))
		for _, device := range devices {
			if device.CfMonitorIcinga {
				monitored = append(monitored, device)
			}
		}
		stats, err := fic.pollOnce(ctx, opt, monitored, store, submit, time.Now())
		if saveErr := store.save(opt.StateFile); saveErr != nil {
			reporter.EmitErr(saveErr)
			if err == nil {
				err = saveErr
			}
		}
		reporter.Emit(jobevent.Info, "polled %d interface(s) on %d device(s) in %s, snmp errors %d, submitted %d",
			stats.Interfaces, stats.Devices, time.Since(started).Round(time.Millisecond), stats.SNMPErrors, stats.Submitted)
		if err != nil {
			reporter.EmitErr(err)
			if opt.Interval <= 0 {
				return err
			}
		}
		if opt.Interval <= 0 {
			return nil
		}
		wait := opt.Interval - time.Since(started)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
