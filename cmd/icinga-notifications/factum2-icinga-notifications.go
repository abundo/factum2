// factum2-icinga-notifications is the Icinga2 NotificationCommand invoked
// directly by icinga2 whenever a host/service alarm fires. It builds an
// HTML alert email (alarm details plus a live "hosts/services currently
// down" summary from the Icinga API) from a Go html/template on disk, and
// sends it over SMTP.
//
// Unlike every other cmd/* binary, this one does NOT use
// github.com/GiGurra/boa/spf13/cobra - Icinga invokes it with its own fixed
// flag shape (-d, -l, -r, -t, --HOSTNAME, a bare --SERVICE sentinel, ...),
// which collides with boa's global -d/-l (Debug/Loglevel, present on every
// other cmd/* binary). Args are parsed directly with
// github.com/jessevdk/go-flags instead, using the same short/long flag
// names as the Python script this replaces so existing Icinga2
// NotificationCommand definitions don't need to change.
package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
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
	// supplies everything above.
	ConfigFile   string `long:"config-file" default:"/etc/factum2/factum2-worker.yaml"`
	TemplateFile string `long:"template-file" default:"/etc/factum2/icinga-notification-email.tpl"`
}

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
		if _, err := flags.NewParser(&p, flags.Default).ParseArgs(rest); err != nil {
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
	if _, err := flags.NewParser(&p, flags.Default).ParseArgs(rest); err != nil {
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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "factum2-icinga-notifications:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Version)
		return nil
	}

	n, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
	}

	writeDebugLog(os.Args)

	config, err := loadConfig(n.ConfigFile)
	if err != nil {
		return fmt.Errorf("loading config %q: %w", n.ConfigFile, err)
	}

	icingaConfig, err := icinga.FetchRemoteConfig(&config.Factum)
	if err != nil {
		return fmt.Errorf("fetching icinga config: %w", err)
	}

	data := buildEmailData(n, icingaConfig.DefaultDomain)
	fetchDownSummaries(icinga.NewIcingaClient(*icingaConfig), &data)

	body, err := renderTemplate(n.TemplateFile, data)
	if err != nil {
		return fmt.Errorf("rendering email template %q: %w", n.TemplateFile, err)
	}

	sender := n.MailFrom
	if sender == "" {
		sender = icingaConfig.CommonConfig.EmailSender
	}

	if err := mail.Send(icingaConfig.CommonConfig, sender, n.UserEmail, n.subject(icingaConfig.DefaultDomain), body); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
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

// writeDebugLog is a best-effort troubleshooting aid, mirroring the Python
// script's argv dump to /tmp/mail_notification.log. Unlike the Python, a
// failure here must never abort sending the actual alert - it's purely
// diagnostic.
func writeDebugLog(args []string) {
	f, err := os.OpenFile("/tmp/mail_notification.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Warn("cannot write notification debug log", "error", err)
		return
	}
	defer f.Close()

	fmt.Fprintln(f, strings.Join(args, " "))
	fmt.Fprintln(f, "arguments:")
	for i, a := range args {
		fmt.Fprintf(f, "  %2d %s\n", i, a)
	}
	fmt.Fprintln(f)
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

	// CustomersDownEstimate is HostsDown's length times a rough
	// per-host customer count - carried over as-is from the original
	// script's hardcoded "20 *" estimate.
	CustomersDownEstimate int
}

type hostDownRow struct {
	Name, Since, Changed, Location, Role, Manufacturer, Model, Notes string
}

type serviceDownRow struct {
	Host, Service, Since, Changed, Output, Notes string
}

// customersPerHostEstimate is the rough "how many customers does one down
// host affect" multiplier the original Python notification script used.
const customersPerHostEstimate = 20

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
		data.CustomersDownEstimate = len(hostsDown.Results) * customersPerHostEstimate
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
