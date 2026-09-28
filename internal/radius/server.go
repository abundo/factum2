package radius

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/ldapauth"
)

const (
	refreshEvery = 30 * time.Second
	reportEvery  = time.Second
	maxInFlight  = 64
	perIPPerSec  = 20
	replayFor    = 5 * time.Second
	maxSpool     = 1000
	authTimeout  = 8 * time.Second
)

// Options wires the listener to the hub. Fetch and Report may fail while
// factum2 is down; the last Config on disk keeps answering.
type Options struct {
	StatePath string
	Worker    string
	Fetch     func(ctx context.Context) ([]byte, error)
	Report    func(ctx context.Context, body []byte) error
	// Authenticate overrides the LDAP bind. Tests set it. Nil uses ldapauth.
	Authenticate func(cfg ldapauth.Config, username, password string) (groups []string, ok bool, err error)
	// VerifyMSCHAP overrides the Active Directory NETLOGON check. Nil uses
	// verifyMSCHAP. A nil error and a 16-byte session key means the response
	// was accepted. ErrBadCredentials is a wrong password.
	VerifyMSCHAP func(ctx context.Context, cfg ldapauth.Config, machine Machine, username string, challenge, ntResponse []byte) ([]byte, error)
	// LookupGroups overrides the directory group search used after MS-CHAPv2.
	LookupGroups func(cfg ldapauth.Config, username string) (groups []string, found bool, err error)
}

// Server answers RADIUS on UDP. The snapshot is swapped as a whole.
type Server struct {
	opt Options

	mu      sync.RWMutex
	idx     *Index
	limiter map[string]int
	window  time.Time
	replay  map[string]replayEntry

	logMu sync.Mutex
	queue []Event
	last  []byte

	pcMu  sync.Mutex
	pc    net.PacketConn
	slots chan struct{}
}

type replayEntry struct {
	auth  [16]byte
	reply []byte
	until time.Time
	done  bool
}

// Run refreshes config, ships the login log, and serves UDP until ctx ends.
func Run(ctx context.Context, opt Options) error {
	if opt.Worker == "" {
		opt.Worker, _ = os.Hostname()
	}
	if opt.Authenticate == nil {
		opt.Authenticate = func(cfg ldapauth.Config, username, password string) ([]string, bool, error) {
			ok, info, err := ldapauth.Authenticate(cfg, username, password)
			if err != nil || !ok || info == nil {
				return nil, ok, err
			}
			return info.Groups, true, nil
		}
	}
	if opt.VerifyMSCHAP == nil {
		opt.VerifyMSCHAP = verifyMSCHAP
	}
	if opt.LookupGroups == nil {
		opt.LookupGroups = ldapauth.LookupGroups
	}
	s := &Server{
		opt:     opt,
		limiter: map[string]int{},
		replay:  map[string]replayEntry{},
		slots:   make(chan struct{}, maxInFlight),
	}
	s.loadCache()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); s.refreshLoop(ctx) }()
	go func() { defer wg.Done(); s.reportLoop(ctx) }()
	go func() { defer wg.Done(); s.listenLoop(ctx) }()
	<-ctx.Done()
	s.closeConn()
	wg.Wait()
	return nil
}

func (s *Server) current() *Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idx
}

func (s *Server) setConfig(cfg Config) {
	idx := BuildIndex(cfg)
	s.mu.Lock()
	s.idx = idx
	s.mu.Unlock()
}

func (s *Server) refreshLoop(ctx context.Context) {
	s.refresh(ctx)
	t := time.NewTicker(refreshEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh(ctx)
		}
	}
}

func (s *Server) refresh(ctx context.Context) {
	if s.opt.Fetch == nil {
		return
	}
	body, err := s.opt.Fetch(ctx)
	if err != nil {
		slog.Debug("radius config refresh failed", "err", err)
		return
	}
	if bytes.Equal(body, s.lastBody()) {
		return
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		slog.Error("radius config rejected", "err", err)
		return
	}
	if _, err := EncodeReply(cfg.Reply); err != nil {
		slog.Error("radius reply attributes rejected", "err", err)
		return
	}
	s.setConfig(cfg)
	s.setLast(body)
	if err := s.saveCache(cfg); err != nil {
		slog.Error("radius config cache", "err", err)
	}
	slog.Info("radius config applied", "enabled", cfg.Enabled, "clients", len(cfg.Clients), "devices", len(cfg.Devices))
}

func (s *Server) lastBody() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

func (s *Server) setLast(body []byte) {
	s.mu.Lock()
	s.last = append([]byte(nil), body...)
	s.mu.Unlock()
}

func (s *Server) loadCache() {
	if s.opt.StatePath == "" {
		return
	}
	b, err := os.ReadFile(s.opt.StatePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("radius config cache unreadable", "err", err)
		}
		return
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		slog.Warn("radius config cache rejected", "err", err)
		return
	}
	if _, err := EncodeReply(cfg.Reply); err != nil {
		slog.Warn("radius config cache reply rejected", "err", err)
		return
	}
	s.setConfig(cfg)
	slog.Info("radius config loaded from disk", "enabled", cfg.Enabled)
}

func (s *Server) saveCache(cfg Config) error {
	if s.opt.StatePath == "" {
		return nil
	}
	dir := filepath.Dir(s.opt.StatePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := s.opt.StatePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.opt.StatePath)
}

func (s *Server) listenLoop(ctx context.Context) {
	var bound string
	for {
		if ctx.Err() != nil {
			return
		}
		idx := s.current()
		if idx == nil || !idx.Enabled {
			if bound != "" {
				s.closeConn()
				bound = ""
			}
			if !sleepCtx(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if bound != idx.Listen {
			s.closeConn()
			pc, err := net.ListenPacket("udp", idx.Listen)
			if err != nil {
				slog.Error("radius listen", "addr", idx.Listen, "err", err)
				if !sleepCtx(ctx, 5*time.Second) {
					return
				}
				continue
			}
			s.pcMu.Lock()
			s.pc = pc
			s.pcMu.Unlock()
			bound = idx.Listen
			slog.Info("radius listening", "addr", idx.Listen)
		}
		s.readOnce(ctx)
	}
}

func (s *Server) readOnce(ctx context.Context) {
	s.pcMu.Lock()
	pc := s.pc
	s.pcMu.Unlock()
	if pc == nil {
		sleepCtx(ctx, time.Second)
		return
	}
	_ = pc.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, maxPacket)
	n, addr, err := pc.ReadFrom(buf)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return
		}
		slog.Debug("radius read", "err", err)
		return
	}
	pkt := append([]byte(nil), buf[:n]...)
	if !s.allow(addr) {
		return
	}
	select {
	case s.slots <- struct{}{}:
		go func() {
			defer func() { <-s.slots }()
			s.handle(pc, addr, pkt)
		}()
	default:
	}
}

func (s *Server) closeConn() {
	s.pcMu.Lock()
	defer s.pcMu.Unlock()
	if s.pc != nil {
		_ = s.pc.Close()
		s.pc = nil
	}
}

func (s *Server) allow(addr net.Addr) bool {
	ip := hostIP(addr)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.window) >= time.Second {
		s.window = now
		s.limiter = map[string]int{}
	}
	s.limiter[ip]++
	return s.limiter[ip] <= perIPPerSec
}

func (s *Server) handle(pc net.PacketConn, addr net.Addr, raw []byte) {
	idx := s.current()
	if idx == nil || !idx.Enabled {
		return
	}
	source := hostIP(addr)
	if reply, proceed := s.begin(source, raw); !proceed {
		if len(reply) > 0 {
			_, _ = pc.WriteTo(reply, addr)
		}
		return
	}
	in, early, ok := gate(raw, source, idx)
	if !ok {
		s.finish(pc, addr, source, early)
		return
	}
	var groups []string
	var ldapOK bool
	var ldapErr error
	var extra []byte
	if in.chap != nil {
		groups, extra, ldapOK, ldapErr = s.mschap(idx, in)
		if errors.Is(ldapErr, errMSCHAPNotAD) || errors.Is(ldapErr, errMSCHAPConfig) {
			out := outcome{
				code: codeAccessReject, result: resultReject, record: true,
				username: in.username, nasIP: canonIP(source), secret: secretFor(idx, source),
				req: packetOf(raw),
			}
			if errors.Is(ldapErr, errMSCHAPNotAD) {
				out.reason = reasonMSCHAPDir
			} else {
				out.reason = reasonMSCHAPConfig
			}
			s.finish(pc, addr, source, out)
			return
		}
	} else {
		groups, ldapOK, ldapErr = s.opt.Authenticate(idx.LDAP, in.username, in.password)
	}
	out := authorize(in.username, in.nas, source, idx, groups, ldapOK, ldapErr)
	out.req = packetOf(raw)
	out.secret = secretFor(idx, source)
	out.extra = extra
	s.finish(pc, addr, source, out)
}

func (s *Server) mschap(idx *Index, in login) (groups []string, extra []byte, ok bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), authTimeout)
	defer cancel()
	chal := challengeHash(in.chap.peer, in.chap.auth, in.chap.user)
	key, err := s.opt.VerifyMSCHAP(ctx, idx.LDAP, idx.machine, in.chap.user, chal, in.chap.nt)
	if errors.Is(err, ErrBadCredentials) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	groups, found, err := s.opt.LookupGroups(idx.LDAP, in.chap.user)
	if err != nil {
		return nil, nil, false, err
	}
	if !found {
		return nil, nil, false, nil
	}
	return groups, mschap2Success(*in.chap, key), true, nil
}

func packetOf(raw []byte) Packet {
	p, _, err := parse(raw)
	if err != nil {
		return Packet{}
	}
	return p
}

func secretFor(idx *Index, source string) string {
	if c, ok := idx.clients[canonIP(source)]; ok {
		return c.Secret
	}
	return ""
}

func (s *Server) finish(pc net.PacketConn, addr net.Addr, source string, out outcome) {
	var reply []byte
	if out.code != 0 && out.secret != "" && out.req.Code != 0 {
		var extra []byte
		if out.code == codeAccessAccept {
			if idx := s.current(); idx != nil {
				extra = append(extra, idx.reply...)
			}
			extra = append(extra, out.extra...)
		}
		reply = response(out.req, out.code, []byte(out.secret), replyMessage(out.reason), extra)
		_, _ = pc.WriteTo(reply, addr)
		s.storeReplay(source, out.req, reply)
	}
	if out.record {
		s.enqueue(out)
	}
}

func replayKey(source string, id byte) string {
	return fmt.Sprintf("%s/%d", source, id)
}

// begin records this request authenticator. proceed is false when a
// duplicate is already running (reply empty) or already answered.
func (s *Server) begin(source string, raw []byte) (reply []byte, proceed bool) {
	if len(raw) < 20 {
		return nil, true
	}
	var auth [16]byte
	copy(auth[:], raw[4:20])
	key := replayKey(source, raw[1])
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.replay[key]; ok && time.Now().Before(e.until) && e.auth == auth {
		if e.done {
			return e.reply, false
		}
		return nil, false
	}
	s.replay[key] = replayEntry{auth: auth, until: time.Now().Add(replayFor)}
	return nil, true
}

func (s *Server) storeReplay(source string, req Packet, reply []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replay[replayKey(source, req.ID)] = replayEntry{
		auth:  req.Auth,
		reply: append([]byte(nil), reply...),
		until: time.Now().Add(replayFor),
		done:  true,
	}
	if len(s.replay) <= 4096 {
		return
	}
	now := time.Now()
	for k, e := range s.replay {
		if now.After(e.until) {
			delete(s.replay, k)
		}
	}
}

func (s *Server) enqueue(out outcome) {
	ev := Event{
		ReportedAt: time.Now().UTC().Format(time.RFC3339Nano),
		NASIP:      out.nasIP,
		DeviceName: out.deviceName,
		DeviceRole: out.deviceRole,
		Result:     out.result,
		Reason:     out.reason,
		Worker:     s.opt.Worker,
	}
	if out.result != resultDrop {
		ev.Username = out.username
	}
	s.logMu.Lock()
	s.queue = append(s.queue, ev)
	s.logMu.Unlock()
}

func (s *Server) reportLoop(ctx context.Context) {
	s.drainSpool(ctx)
	t := time.NewTicker(reportEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.flush(context.Background())
			return
		case <-t.C:
			s.flush(ctx)
		}
	}
}

func (s *Server) flush(ctx context.Context) {
	s.logMu.Lock()
	batch := s.queue
	s.queue = nil
	s.logMu.Unlock()
	if len(batch) == 0 || s.opt.Report == nil {
		if s.opt.Report == nil && len(batch) > 0 {
			s.spool(batch)
		}
		return
	}
	if len(batch) > 100 {
		s.spool(batch[100:])
		batch = batch[:100]
	}
	body, err := json.Marshal(struct {
		Events []Event `json:"events"`
	}{Events: batch})
	if err != nil {
		return
	}
	if err := s.opt.Report(ctx, body); err != nil {
		slog.Debug("radius log ship failed", "err", err)
		s.spool(batch)
	}
}

func (s *Server) spoolPath() string {
	if s.opt.StatePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.opt.StatePath), "events.jsonl")
}

func (s *Server) spool(events []Event) {
	path := s.spoolPath()
	if path == "" || len(events) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		slog.Error("radius log spool", "err", err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		slog.Error("radius log spool", "err", err)
		return
	}
	defer f.Close()
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		_, _ = f.Write(append(line, '\n'))
	}
	s.trimSpool(path)
}

func (s *Server) trimSpool(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := splitLines(b)
	if len(lines) <= maxSpool {
		return
	}
	lines = lines[len(lines)-maxSpool:]
	_ = os.WriteFile(path, []byte(joinLines(lines)), 0o600)
}

func (s *Server) drainSpool(ctx context.Context) {
	path := s.spoolPath()
	if path == "" || s.opt.Report == nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var events []Event
	for _, line := range splitLines(b) {
		var ev Event
		if json.Unmarshal(line, &ev) == nil && ev.Result != "" {
			events = append(events, ev)
		}
	}
	if len(events) == 0 {
		return
	}
	body, err := json.Marshal(struct {
		Events []Event `json:"events"`
	}{Events: events})
	if err != nil {
		return
	}
	if err := s.opt.Report(ctx, body); err != nil {
		return
	}
	_ = os.Remove(path)
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

func joinLines(lines [][]byte) string {
	var b []byte
	for _, l := range lines {
		b = append(b, l...)
		b = append(b, '\n')
	}
	return string(b)
}

func hostIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return canonIP(addr.String())
	}
	return canonIP(host)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// StatePath is the on-disk config cache. Empty uses the default under
// /var/lib/factum2. The file holds directory and NAS secrets.
func StatePath(override string) string {
	if override != "" {
		return override
	}
	return "/var/lib/factum2/radius/cache.json"
}
