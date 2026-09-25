// factum2-icinga-notifications is the Icinga2 NotificationCommand invoked
// directly by icinga2 whenever a host/service alarm fires. It builds an
// HTML alert email (alarm details, the factum customers and services on
// the alarming host, plus a live "hosts/services currently down" summary
// from the Icinga API) from a Go html/template on disk, and sends it over
// SMTP. The factum lookup is GET /api/device/name/:name/impact over the
// factum2-worker unix socket (HTTPS bearer is the fallback).
//
// Unlike every other cmd/* binary, this one does NOT use
// github.com/GiGurra/boa/spf13/cobra - Icinga invokes it with its own fixed
// flag shape (-d, -l, -r, -t, --HOSTNAME, a bare --SERVICE sentinel, ...),
// which collides with boa's global -d/-l (Debug/Loglevel, present on every
// other cmd/* binary). Args are parsed directly with
// github.com/jessevdk/go-flags instead, using the same short/long flag
// names as the Python script this replaces so existing Icinga2
// NotificationCommand definitions don't need to change.
//
// Do not enable Icinga2's debuglog to troubleshoot this command - that
// file records every check execution and the full argv (including huge
// -o plugin output). This binary writes a compact per-run log of its
// own (see --debug-log) and a one-line syslog result on failure, or on
// success when -v/--SYSLOG is true. --dry-run renders the email to
// stdout without sending.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/syslog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	goyaml "github.com/goccy/go-yaml"
	flags "github.com/jessevdk/go-flags"

	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/icinga"
	"github.com/abundo/factum2/internal/mail"
	"github.com/abundo/factum2/internal/util"
)

// --------------------------------------------------------------------------
//
// # Argument parsing
//
// --------------------------------------------------------------------------

// commonArgs holds every flag shared by both host and service
// notifications.
type commonArgs struct {
	LongDateTime     string `short:"d" long:"LONGDATETIME" required:"true"`
	HostName         string `short:"l" long:"HOSTNAME" required:"true"`
	HostDisplayName  string `short:"n" long:"HOSTDISPLAYNAME" required:"true"`
	UserEmail        string `short:"r" long:"USEREMAIL" required:"true"`
	NotificationType string `short:"t" long:"NOTIFICATIONTYPE" required:"true"`

	HostAddress            string `short:"4" long:"HOSTADDRESS"`
	HostAddress6           string `short:"6" long:"HOSTADDRESS6"`
	NotificationAuthorName string `short:"b" long:"NOTIFICATIONAUTHORNAME"`
	NotificationComment    string `short:"c" long:"NOTIFICATIONCOMMENT"`
	IcingaWeb2URL          string `short:"i" long:"ICINGAWEB2URL"`
	MailFrom               string `short:"f" long:"MAILFROM"`
	Syslog                 string `short:"v" long:"SYSLOG"`
	Icinga2Host            string `long:"ICINGA2HOST"`

	FactumComments     string `long:"factum_comments"`
	FactumLocation     string `long:"factum_location"`
	FactumManufacturer string `long:"factum_manufacturer"`
	FactumModel        string `long:"factum_model"`
	FactumParents      string `long:"factum_parents"`
	FactumPlatform     string `long:"factum_platform"`
	FactumRole         string `long:"factum_role"`
	FactumSiteName     string `long:"factum_site_name"`

	// ConfigFile/TemplateFile aren't sent by Icinga - they're this binary's
	// own flags, added to the same NotificationCommand definition that
	// supplies everything above. DebugLog/DryRun are the same: operators
	// pass them on a manual invocation, not from Icinga macros.
	ConfigFile   string `long:"config-file" default:"/etc/factum2/factum2-worker.yaml"`
	TemplateFile string `long:"template-file" default:"/etc/factum2/icinga-notification-email.tpl"`
	DebugLog     string `long:"debug-log"`
	DryRun       bool   `long:"dry-run"`
}

// parseOpts omits flags.PrintErrors so a missing Icinga macro does not dump
// the full usage wall into Icinga's captured stderr (and thus debug.log).
const parseOpts = flags.HelpFlag | flags.PassDoubleDash

var (
	fetchIcingaConfig = icinga.FetchRemoteConfig
	newIcingaClient   = func(c util.ConfigIcinga) icingaDownFetcher {
		return icinga.NewIcingaClient(c)
	}
	fetchAffected = fetchAffectedHTTP
	mailSend      = mail.Send
)

// errDeviceNotInFactum means every candidate host name 404'd. The alarm
// email still goes out; the template says the host was not found.
var errDeviceNotInFactum = errors.New("device not found in factum")

type hostArgs struct {
	commonArgs
	HostOutput string `short:"o" long:"HOSTOUTPUT" required:"true"`
	HostState  string `short:"s" long:"HOSTSTATE" required:"true"`
}

type serviceArgs struct {
	commonArgs
	ServiceName        string `short:"e" long:"SERVICENAME" required:"true"`
	ServiceOutput      string `short:"o" long:"SERVICEOUTPUT" required:"true"`
	ServiceState       string `short:"s" long:"SERVICESTATE" required:"true"`
	ServiceDisplayName string `short:"u" long:"SERVICEDISPLAYNAME" required:"true"`
}

// notification is the parsed result, host and service args flattened into
// one shape the rest of the program works with.
type notification struct {
	commonArgs
	IsService bool

	HostOutput string
	HostState  string

	ServiceName        string
	ServiceOutput      string
	ServiceState       string
	ServiceDisplayName string
}

// detectServiceMode scans args for the bare "--SERVICE" sentinel Icinga2's
// service NotificationCommand definitions add (no value, just a marker) and
// strips it - the go-flags struct used to parse the rest depends on whether
// it was present.
func detectServiceMode(args []string) (isService bool, rest []string) {
	rest = make([]string, 0, len(args))
	for _, a := range args {
		if a == "--SERVICE" {
			isService = true
			continue
		}
		rest = append(rest, a)
	}
	return isService, rest
}

func parseArgs(args []string) (notification, error) {
	isService, rest := detectServiceMode(args)

	var n notification
	n.IsService = isService

	if isService {
		var p serviceArgs
		if _, err := flags.NewParser(&p, parseOpts).ParseArgs(rest); err != nil {
			return n, err
		}
		n.commonArgs = p.commonArgs
		n.ServiceName = p.ServiceName
		n.ServiceOutput = p.ServiceOutput
		n.ServiceState = p.ServiceState
		n.ServiceDisplayName = p.ServiceDisplayName
		return n, nil
	}

	var p hostArgs
	if _, err := flags.NewParser(&p, parseOpts).ParseArgs(rest); err != nil {
		return n, err
	}
	n.commonArgs = p.commonArgs
	n.HostOutput = p.HostOutput
	n.HostState = p.HostState
	return n, nil
}

// subject builds the notification email's subject line.
func (n notification) subject(defaultDomain string) string {
	host := util.ShortName(n.HostDisplayName, defaultDomain)
	if n.IsService {
		return fmt.Sprintf("%s, Host '%s', Service '%s' is in state '%s' !",
			n.NotificationType, host, n.ServiceDisplayName, n.ServiceState)
	}
	return fmt.Sprintf("%s, Host '%s' is in state '%s' !", n.NotificationType, host, n.HostState)
}

// --------------------------------------------------------------------------
//
// # main
//
// --------------------------------------------------------------------------

func main() {
	if err := runArgs(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "factum2-icinga-notifications:", err)
		os.Exit(1)
	}
}

func runArgs(args []string) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(buildinfo.Version)
		return nil
	}

	rl := newRunLogger(peekFlag(args, "--debug-log"), syslogEnabled(peekFlag(args, "-v", "--SYSLOG")))
	defer rl.Close()
	rl.write(fmt.Sprintf("start uid=%d gid=%d debug-log=%s args=%s", os.Getuid(), os.Getgid(), rl.path, summarizeArgs(args)), false)

	n, err := parseArgs(args)
	if err != nil {
		rl.fail("parse", err)
		return err
	}
	if syslogEnabled(n.Syslog) && rl.syslog == nil {
		rl.syslog = openSyslog()
	}

	state := n.HostState
	service := ""
	if n.IsService {
		state = n.ServiceState
		service = n.ServiceDisplayName
	}
	rl.write(fmt.Sprintf("notification type=%s host=%s service=%s state=%s to=%s dry-run=%t",
		n.NotificationType, n.HostName, service, state, n.UserEmail, n.DryRun), false)

	config, err := loadConfig(n.ConfigFile)
	if err != nil {
		err = fmt.Errorf("loading config %q: %w", n.ConfigFile, err)
		rl.fail("config", err)
		return err
	}
	rl.write(fmt.Sprintf("config path=%s %s", n.ConfigFile, socketStatus(&config.Factum)), false)

	icingaConfig, err := fetchIcingaConfig(&config.Factum)
	if err != nil {
		err = fmt.Errorf("fetching icinga config: %w", err)
		rl.fail("remote-config", err)
		return err
	}
	rl.write(fmt.Sprintf("remote-config smtp_host=%s smtp_port=%d smtp_tls=%s sender=%s icinga_api=%s",
		icingaConfig.SmtpHost, icingaConfig.SmtpPort, icingaConfig.SmtpTLSMode,
		icingaConfig.EmailSender, icingaConfig.URL), false)

	data := buildEmailData(n, icingaConfig.DefaultDomain)
	fetchDownSummaries(newIcingaClient(*icingaConfig), &data)
	rl.write(fmt.Sprintf("down-summaries hosts=%d services=%d hosts_err=%s services_err=%s",
		len(data.HostsDown), len(data.ServicesDown),
		strconv.Quote(data.HostsDownError), strconv.Quote(data.ServicesDownError)), false)

	names := impactNames(n.HostName, icingaConfig.DefaultDomain)
	affected, err := fetchAffected(&config.Factum, names)
	if err != nil {
		if errors.Is(err, errDeviceNotInFactum) {
			data.AffectedMissing = true
		} else {
			data.AffectedError = err.Error()
		}
	} else {
		data.AffectedDevice = affected.DeviceName
		data.AffectedCustomers = affected.Customers
		data.AffectedServices = affected.Services
	}
	rl.write(fmt.Sprintf("affected names=%s device=%s customers=%d services=%d missing=%t err=%s",
		strings.Join(names, ","), data.AffectedDevice,
		len(data.AffectedCustomers), len(data.AffectedServices),
		data.AffectedMissing, strconv.Quote(data.AffectedError)), false)

	body, err := renderTemplate(n.TemplateFile, data)
	if err != nil {
		err = fmt.Errorf("rendering email template %q: %w", n.TemplateFile, err)
		rl.fail("template", err)
		return err
	}
	rl.write(fmt.Sprintf("template path=%s bytes=%d", n.TemplateFile, len(body)), false)

	sender := n.MailFrom
	if sender == "" {
		sender = icingaConfig.CommonConfig.EmailSender
	}
	subj := n.subject(icingaConfig.DefaultDomain)

	if n.DryRun {
		fmt.Fprintf(os.Stdout, "From: %s\nTo: %s\nSubject: %s\n\n%s", sender, n.UserEmail, subj, body)
		if !strings.HasSuffix(body, "\n") {
			fmt.Fprintln(os.Stdout)
		}
		rl.ok("dry-run", sender, n.UserEmail, subj)
		return nil
	}

	if err := mailSend(icingaConfig.CommonConfig, sender, n.UserEmail, subj, body); err != nil {
		err = fmt.Errorf("sending email: %w", err)
		rl.fail("smtp", err)
		return err
	}
	rl.ok("sent", sender, n.UserEmail, subj)
	return nil
}

// loadConfig reads this host's local util.ConfigAgentRoot (just
// factum.url/token - everything else, including the SMTP relay settings,
// comes from the primary over REST, same as every other cmd/icinga
// subcommand).
func loadConfig(path string) (*util.ConfigAgentRoot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config util.ConfigAgentRoot
	if err := goyaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// --------------------------------------------------------------------------
//
// # Run log
//
// Icinga2's debug.log is the wrong place to debug this command: it records
// every check, API call, and the full NotificationCommand argv (including
// huge -o plugin output). A failed notification still shows up as
// warning/PluginNotificationTask in icinga2.log; everything else belongs
// here. systemd PrivateTmp on icinga2.service also hides /tmp from the
// host, so we prefer Icinga's own log directory over /tmp.
//
// --------------------------------------------------------------------------

var defaultDebugLogPaths = []string{
	"/var/log/icinga2/factum2-icinga-notifications.log",
	"/var/log/factum2/icinga-notifications.log",
	"/tmp/mail_notification.log",
}

const argValueMax = 80

type runLogger struct {
	w      io.Writer
	closer io.Closer
	syslog *syslog.Writer
	start  time.Time
	path   string
}

func newRunLogger(explicitPath string, wantSyslog bool) *runLogger {
	l := &runLogger{start: time.Now(), path: "none"}
	if f, path, err := openDebugLogFile(explicitPath); err == nil && f != nil {
		l.w = f
		l.closer = f
		l.path = path
	}
	if wantSyslog {
		l.syslog = openSyslog()
	}
	return l
}

func (l *runLogger) Close() {
	if l.syslog != nil {
		l.syslog.Close()
		l.syslog = nil
	}
	if l.closer != nil {
		l.closer.Close()
		l.closer = nil
	}
}

func (l *runLogger) write(msg string, fail bool) {
	line := time.Now().UTC().Format(time.RFC3339) + " " + msg
	if l.w != nil {
		fmt.Fprintln(l.w, line)
	}
	if l.syslog != nil {
		if fail {
			_ = l.syslog.Err(msg)
		} else {
			_ = l.syslog.Info(msg)
		}
		return
	}
	if fail {
		if w := openSyslog(); w != nil {
			_ = w.Err(msg)
			w.Close()
		}
	}
}

func (l *runLogger) fail(step string, err error) {
	l.write(fmt.Sprintf("result=fail step=%s err=%s duration=%s",
		step, strconv.Quote(err.Error()), time.Since(l.start).Round(time.Millisecond)), true)
}

func (l *runLogger) ok(result, from, to, subject string) {
	l.write(fmt.Sprintf("result=%s from=%s to=%s subject=%s duration=%s",
		result, from, to, strconv.Quote(subject), time.Since(l.start).Round(time.Millisecond)), false)
}

func openDebugLogFile(explicit string) (*os.File, string, error) {
	if explicit == "none" || explicit == "0" {
		return nil, "", nil
	}
	paths := defaultDebugLogPaths
	if explicit != "" {
		paths = []string{explicit}
	}
	var errs []string
	for _, p := range paths {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err == nil {
			return f, p, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", p, err))
	}
	return nil, "", fmt.Errorf("%s", strings.Join(errs, "; "))
}

var openSyslog = func() *syslog.Writer {
	w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_USER, "factum2-icinga-notifications")
	if err != nil {
		return nil
	}
	return w
}

func syslogEnabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// peekFlag returns the value of the first matching --name / --name=value /
// -x value flag. Used to open the run log before go-flags parse, so a
// missing required macro is still recorded.
func peekFlag(args []string, names ...string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		for _, n := range names {
			if a == n {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					return args[i+1]
				}
				return ""
			}
			prefix := n + "="
			if strings.HasPrefix(a, prefix) {
				return strings.TrimPrefix(a, prefix)
			}
		}
	}
	return ""
}

func summarizeArgs(args []string) string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			out = append(out, a, truncateArg(args[i+1]))
			i++
			continue
		}
		if j := strings.IndexByte(a, '='); j >= 0 && strings.HasPrefix(a, "-") {
			out = append(out, a[:j+1]+truncateArg(a[j+1:]))
			continue
		}
		out = append(out, truncateArg(a))
	}
	return strings.Join(out, " ")
}

func truncateArg(s string) string {
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	if len(s) <= argValueMax {
		return s
	}
	return fmt.Sprintf("%s...<%d bytes>", s[:argValueMax], len(s))
}

func socketStatus(cfg *util.ConfigFactum) string {
	p := util.HubSocketPath(cfg.Socket)
	if p == "" {
		return "socket=disabled"
	}
	fi, err := os.Stat(p)
	if err != nil {
		return fmt.Sprintf("socket=%s err=%s", p, strconv.Quote(err.Error()))
	}
	return fmt.Sprintf("socket=%s mode=%s", p, fi.Mode())
}

// --------------------------------------------------------------------------
//
// # Email template data
//
// --------------------------------------------------------------------------

type emailData struct {
	IsService           bool
	NotificationType    string
	HostName            string
	HostState           string
	ServiceDisplayName  string
	ServiceState        string
	Info                string // host or service check output
	When                string // LONGDATETIME with the trailing timezone token stripped
	NotificationAuthor  string
	NotificationComment string

	// Hardware/Other sections - host notifications only.
	Location, SiteName, Role, Manufacturer, Model string
	Parents                                       string // pre-joined, ", "-separated
	Comments, Platform                            string
	HostAddress, HostAddress6                     string

	IcingaLink      string
	IcingaLinkLabel string

	HostsDown      []hostDownRow
	HostsDownError string

	ServicesDown      []serviceDownRow
	ServicesDownError string

	// Customers and services factum says ride on this host. A failed
	// lookup is recorded and does not block the alarm email.
	AffectedDevice    string
	AffectedCustomers []string
	AffectedServices  []affectedService
	AffectedMissing   bool
	AffectedError     string
}

type affectedService struct {
	ServiceID string
	Customer  string
	Category  string
}

type affectedImpact struct {
	DeviceName string
	Customers  []string
	Services   []affectedService
}

type hostDownRow struct {
	Name, Since, Changed, Location, Role, Manufacturer, Model, Notes string
}

type serviceDownRow struct {
	Host, Service, Since, Changed, Output, Notes string
}

func buildEmailData(n notification, defaultDomain string) emailData {
	when := strings.Fields(n.LongDateTime)
	if len(when) > 0 {
		when = when[:len(when)-1] // drop the trailing timezone token
	}

	data := emailData{
		IsService:           n.IsService,
		NotificationType:    n.NotificationType,
		HostName:            util.ShortName(n.HostDisplayName, defaultDomain),
		HostState:           n.HostState,
		ServiceDisplayName:  n.ServiceDisplayName,
		ServiceState:        n.ServiceState,
		When:                strings.Join(when, " "),
		NotificationAuthor:  n.NotificationAuthorName,
		NotificationComment: n.NotificationComment,
		HostAddress:         n.HostAddress,
		HostAddress6:        n.HostAddress6,
	}

	if n.IsService {
		data.Info = n.ServiceOutput
	} else {
		data.Info = n.HostOutput
		data.Location = n.FactumLocation
		data.SiteName = n.FactumSiteName
		data.Role = n.FactumRole
		data.Manufacturer = n.FactumManufacturer
		data.Model = n.FactumModel
		data.Comments = n.FactumComments
		data.Platform = n.FactumPlatform
		data.Parents = shortParents(n.FactumParents, defaultDomain)
	}

	if n.IcingaWeb2URL != "" {
		if n.IsService {
			data.IcingaLink, data.IcingaLinkLabel = icingaServiceLink(n.IcingaWeb2URL, n.HostName, n.ServiceName)
		} else {
			data.IcingaLink, data.IcingaLinkLabel = icingaHostLink(n.IcingaWeb2URL, n.HostName)
		}
	}

	return data
}

// shortParents shortens a comma-separated list of FQDNs into a
// ", "-joined display string.
func shortParents(parents, defaultDomain string) string {
	if parents == "" {
		return ""
	}
	var short []string
	for _, p := range strings.Split(parents, ",") {
		if p = strings.TrimSpace(p); p != "" {
			short = append(short, util.ShortName(p, defaultDomain))
		}
	}
	return strings.Join(short, ", ")
}

func icingaHostLink(base, hostName string) (href, label string) {
	base = strings.TrimRight(base, "/")
	return fmt.Sprintf("%s/monitoring/host/show?host=%s", base, url.QueryEscape(hostName)), "Open host in Icinga"
}

func icingaServiceLink(base, hostName, serviceName string) (href, label string) {
	base = strings.TrimRight(base, "/")
	return fmt.Sprintf("%s/monitoring/list/services?service_problem=1#!/monitoring/service/show?host=%s&service=%s",
		base, url.QueryEscape(hostName), url.QueryEscape(serviceName)), "Open service in Icinga"
}

// icingaDownFetcher is satisfied by *icinga.icingaClient (unexported, so it
// can't be named directly from this package) - just what fetchDownSummaries
// needs.
type icingaDownFetcher interface {
	GetHostsDown() (*icinga.HostStateResult, error)
	GetServicesDown() (*icinga.ServiceStateResult, error)
}

const downTimeLayout = "2006-01-02 15:04:05"

// fetchDownSummaries fills in the "hosts/services currently down" tables.
// A fetch error is recorded on the data instead of returned - a broken
// Icinga API query must not block the actual alarm email from being sent.
func fetchDownSummaries(client icingaDownFetcher, data *emailData) {
	now := time.Now()

	hostsDown, err := client.GetHostsDown()
	if err != nil {
		data.HostsDownError = err.Error()
	} else {
		for _, h := range hostsDown.Results {
			data.HostsDown = append(data.HostsDown, hostDownRow{
				Name:         h.Name,
				Since:        humanDuration(now.Sub(h.LastHardStateChanged)),
				Changed:      h.LastHardStateChanged.Format(downTimeLayout),
				Location:     h.FactumLocation,
				Role:         h.FactumRole,
				Manufacturer: h.FactumManufacturer,
				Model:        h.FactumModel,
				Notes:        h.Notes,
			})
		}
	}

	servicesDown, err := client.GetServicesDown()
	if err != nil {
		data.ServicesDownError = err.Error()
	} else {
		for _, s := range servicesDown.Results {
			data.ServicesDown = append(data.ServicesDown, serviceDownRow{
				Host:    s.HostName,
				Service: s.Name,
				Since:   humanDuration(now.Sub(s.LastHardStateChanged)),
				Changed: s.LastHardStateChanged.Format(downTimeLayout),
				Output:  s.Output,
				Notes:   s.Notes,
			})
		}
	}
}

// impactNames is the factum device-name candidates for an Icinga host.
// Host objects are fqdn(device.Name), so the object name, the short name,
// and a default-domain qualification are all tried, in that order.
func impactNames(host, defaultDomain string) []string {
	host = strings.TrimSpace(host)
	var names []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, existing := range names {
			if strings.EqualFold(existing, s) {
				return
			}
		}
		names = append(names, s)
	}
	add(host)
	add(util.ShortName(host, defaultDomain))
	add(util.FormatName(defaultDomain, host))
	return names
}

type factumImpactBody struct {
	Services []struct {
		ServiceRef string `json:"service_id"`
		Category   string `json:"category"`
		Customer   string `json:"customer"`
	} `json:"services"`
}

func fetchAffectedHTTP(cfg *util.ConfigFactum, names []string) (affectedImpact, error) {
	if len(names) == 0 {
		return affectedImpact{}, errDeviceNotInFactum
	}
	client, baseURL, viaSocket, err := util.FactumHTTP(cfg)
	if err != nil {
		return affectedImpact{}, err
	}
	for _, name := range names {
		path := "/api/device/name/" + url.PathEscape(name) + "/impact"
		req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
		if err != nil {
			return affectedImpact{}, err
		}
		if !viaSocket {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return affectedImpact{}, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return affectedImpact{}, readErr
		}
		if resp.StatusCode == http.StatusNotFound {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return affectedImpact{}, fmt.Errorf("factum impact %s: %s", path, resp.Status)
		}
		var parsed factumImpactBody
		if err := json.Unmarshal(body, &parsed); err != nil {
			return affectedImpact{}, fmt.Errorf("factum impact %s: %w", path, err)
		}
		return applyFactumImpact(name, parsed), nil
	}
	return affectedImpact{}, errDeviceNotInFactum
}

func applyFactumImpact(deviceName string, body factumImpactBody) affectedImpact {
	out := affectedImpact{DeviceName: deviceName}
	seenCustomer := map[string]bool{}
	for _, row := range body.Services {
		out.Services = append(out.Services, affectedService{
			ServiceID: row.ServiceRef,
			Customer:  row.Customer,
			Category:  row.Category,
		})
		if row.Customer != "" && !seenCustomer[row.Customer] {
			seenCustomer[row.Customer] = true
			out.Customers = append(out.Customers, row.Customer)
		}
	}
	sort.Strings(out.Customers)
	sort.Slice(out.Services, func(i, j int) bool {
		if out.Services[i].Customer != out.Services[j].Customer {
			return out.Services[i].Customer < out.Services[j].Customer
		}
		if out.Services[i].ServiceID != out.Services[j].ServiceID {
			return out.Services[i].ServiceID < out.Services[j].ServiceID
		}
		return out.Services[i].Category < out.Services[j].Category
	})
	return out
}

// humanDuration formats a duration as "1d 2h", "2h 3m" or "5m" - compact
// enough for a table cell.
func humanDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// --------------------------------------------------------------------------
//
// # Template rendering
//
// --------------------------------------------------------------------------

// nl2br replicates the Python script's per-line html.escape() + <br>-join
// behavior for multi-line fields (comments) - it must escape each line
// itself before returning template.HTML, since returning raw template.HTML
// skips html/template's autoescaping entirely.
func nl2br(s string) template.HTML {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = template.HTMLEscapeString(line)
	}
	return template.HTML(strings.Join(lines, "<br>"))
}

var templateFuncs = template.FuncMap{
	"nl2br": nl2br,
}

func renderTemplate(path string, data emailData) (string, error) {
	tmpl, err := template.New(filepath.Base(path)).Funcs(templateFuncs).ParseFiles(path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
