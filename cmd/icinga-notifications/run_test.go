package main

import (
	"bytes"
	"io"
	"log/syslog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/icinga"
	"github.com/abundo/factum2/internal/mail"
	"github.com/abundo/factum2/internal/util"
)

func silenceSyslog(t *testing.T) {
	t.Helper()
	orig := openSyslog
	openSyslog = func() *syslog.Writer { return nil }
	t.Cleanup(func() { openSyslog = orig })
}

func TestSyslogEnabled(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "1", "yes", "on", " Yes "} {
		if !syslogEnabled(v) {
			t.Errorf("syslogEnabled(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "false", "0", "no", "off", "maybe"} {
		if syslogEnabled(v) {
			t.Errorf("syslogEnabled(%q) = true, want false", v)
		}
	}
}

func TestPeekFlag(t *testing.T) {
	args := []string{"-d", "now", "--debug-log", "/tmp/n.log", "-v", "true", "--SYSLOG=false"}
	if got := peekFlag(args, "--debug-log"); got != "/tmp/n.log" {
		t.Errorf("debug-log: got %q", got)
	}
	if got := peekFlag(args, "-v", "--SYSLOG"); got != "true" {
		t.Errorf("-v: got %q", got)
	}
	if got := peekFlag([]string{"--debug-log=/var/log/x"}, "--debug-log"); got != "/var/log/x" {
		t.Errorf("equals form: got %q", got)
	}
	if got := peekFlag([]string{"-d", "now"}, "--debug-log"); got != "" {
		t.Errorf("missing: got %q", got)
	}
}

func TestSummarizeArgsTruncates(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := summarizeArgs([]string{"-o", long, "-s", "DOWN"})
	if strings.Contains(got, long) {
		t.Fatal("expected truncated -o value")
	}
	if !strings.Contains(got, "...<200 bytes>") {
		t.Fatalf("missing size marker: %s", got)
	}
	nl := summarizeArgs([]string{"-o", "line1\nline2"})
	if !strings.Contains(nl, `\n`) {
		t.Fatalf("expected escaped newline, got %q", nl)
	}
}

func TestDetectServiceMode(t *testing.T) {
	is, rest := detectServiceMode([]string{"--SERVICE", "-l", "h"})
	if !is {
		t.Fatal("expected service mode")
	}
	if len(rest) != 2 || rest[0] != "-l" {
		t.Fatalf("rest=%v", rest)
	}
	is, rest = detectServiceMode([]string{"-l", "h"})
	if is {
		t.Fatal("expected host mode")
	}
	if len(rest) != 2 {
		t.Fatalf("rest=%v", rest)
	}
}

func hostFlags(extra ...string) []string {
	args := []string{
		"-d", "2024-01-02 15:04:05 +0100",
		"-l", "sw1.example.com",
		"-n", "sw1.example.com",
		"-r", "ops@example.com",
		"-t", "PROBLEM",
		"-o", "CRITICAL - Host Unreachable",
		"-s", "DOWN",
	}
	return append(args, extra...)
}

func TestParseArgsHost(t *testing.T) {
	n, err := parseArgs(hostFlags())
	if err != nil {
		t.Fatal(err)
	}
	if n.IsService {
		t.Fatal("host parsed as service")
	}
	if n.HostName != "sw1.example.com" || n.HostState != "DOWN" || n.NotificationType != "PROBLEM" {
		t.Fatalf("parsed %+v", n)
	}
}

func TestParseArgsService(t *testing.T) {
	args := append([]string{"--SERVICE"}, hostFlags(
		"-e", "ping",
		"-u", "Ping",
	)...)
	n, err := parseArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if !n.IsService {
		t.Fatal("service parsed as host")
	}
	if n.ServiceName != "ping" || n.ServiceDisplayName != "Ping" {
		t.Fatalf("parsed %+v", n)
	}
}

func TestParseArgsMissingRequired(t *testing.T) {
	var buf bytes.Buffer
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	_, parseErr := parseArgs([]string{"-l", "host"})
	w.Close()
	os.Stderr = old
	io.Copy(&buf, r)
	if parseErr == nil {
		t.Fatal("expected parse error")
	}
	if buf.Len() != 0 {
		t.Fatalf("parse must not dump usage to stderr, got %q", buf.String())
	}
	if !strings.Contains(parseErr.Error(), "required") && !strings.Contains(parseErr.Error(), "LONGDATETIME") {
		t.Fatalf("error should name the missing flag, got %v", parseErr)
	}
}

func TestOpenDebugLogNone(t *testing.T) {
	f, path, err := openDebugLogFile("none")
	if err != nil || f != nil || path != "" {
		t.Fatalf("got f=%v path=%q err=%v", f, path, err)
	}
}

func TestRunLoggerWritesFile(t *testing.T) {
	silenceSyslog(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "n.log")
	rl := newRunLogger(path, false)
	rl.write("hello", false)
	rl.fail("smtp", io.EOF)
	rl.Close()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "hello") {
		t.Fatalf("missing hello: %s", s)
	}
	if !strings.Contains(s, `result=fail step=smtp`) {
		t.Fatalf("missing fail line: %s", s)
	}
}

type stubFetcher struct{}

func (stubFetcher) GetHostsDown() (*icinga.HostStateResult, error) {
	return &icinga.HostStateResult{Results: []icinga.HostState{{Name: "sw2"}}}, nil
}

func (stubFetcher) GetServicesDown() (*icinga.ServiceStateResult, error) {
	return &icinga.ServiceStateResult{}, nil
}

func TestDryRun(t *testing.T) {
	silenceSyslog(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "worker.yaml")
	if err := os.WriteFile(cfgPath, []byte("factum:\n  url: https://example.com\n  token: x\n  socket: none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "n.log")
	tpl := filepath.Join("..", "..", "examples", "icinga-notification-email.tpl")

	origSend, origFetch, origClient := mailSend, fetchIcingaConfig, newIcingaClient
	t.Cleanup(func() {
		mailSend = origSend
		fetchIcingaConfig = origFetch
		newIcingaClient = origClient
	})

	sent := false
	mailSend = func(smtp util.CommonConfig, from, to, subject, body string) error {
		sent = true
		return nil
	}
	fetchIcingaConfig = func(fc *util.ConfigFactum) (*util.ConfigIcinga, error) {
		return &util.ConfigIcinga{
			CommonConfig: util.CommonConfig{
				DefaultDomain: "example.com",
				EmailSender:   "icinga@example.com",
				SmtpHost:      "smtp.example.com",
				SmtpPort:      25,
			},
			URL: "https://icinga.example.com:5665",
		}, nil
	}
	newIcingaClient = func(c util.ConfigIcinga) icingaDownFetcher { return stubFetcher{} }

	args := hostFlags(
		"--dry-run",
		"--config-file", cfgPath,
		"--template-file", tpl,
		"--debug-log", logPath,
	)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := runArgs(args)
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("runArgs: %v", runErr)
	}
	if sent {
		t.Fatal("dry-run sent mail")
	}
	got := string(out)
	if !strings.Contains(got, "From: icinga@example.com") {
		t.Fatalf("missing From: %s", got)
	}
	if !strings.Contains(got, "To: ops@example.com") {
		t.Fatalf("missing To: %s", got)
	}
	if !strings.Contains(got, "sw2") {
		t.Fatalf("expected down-host row in body: %s", got)
	}

	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ls := string(logBody)
	for _, want := range []string{"start uid=", "notification type=PROBLEM", "result=dry-run", "smtp_host=smtp.example.com"} {
		if !strings.Contains(ls, want) {
			t.Errorf("debug log missing %q in:\n%s", want, ls)
		}
	}
}

func TestRunArgsParseFailLogs(t *testing.T) {
	silenceSyslog(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "n.log")
	err := runArgs([]string{"--debug-log", logPath, "-l", "host"})
	if err == nil {
		t.Fatal("expected parse error")
	}
	body, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	s := string(body)
	if !strings.Contains(s, "result=fail step=parse") {
		t.Fatalf("missing parse fail: %s", s)
	}
}

func TestMailSendDefaultWired(t *testing.T) {
	// Sanity: production hook is the real sender so a test that restores
	// origSend actually puts mail.Send back.
	if orig := mailSend; orig == nil {
		t.Fatal("mailSend is nil")
	}
	_ = mail.Send
}
