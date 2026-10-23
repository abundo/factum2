package drivers

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHClientConfigOffersLegacyCBCAfterModernCiphers(t *testing.T) {
	cfg := sshClientConfig("user", "password")
	modern := ssh.SupportedAlgorithms().Ciphers
	if len(cfg.Ciphers) < len(modern) {
		t.Fatalf("ciphers %v shorter than supported %v", cfg.Ciphers, modern)
	}
	for i, want := range modern {
		if cfg.Ciphers[i] != want {
			t.Fatalf("cipher[%d]=%q, want %q (full %v)", i, cfg.Ciphers[i], want, cfg.Ciphers)
		}
	}
	legacy := cfg.Ciphers[len(modern):]
	if !slices.Equal(legacy, []string{ssh.InsecureCipherAES128CBC, ssh.InsecureCipherTripleDESCBC}) {
		t.Fatalf("legacy ciphers = %v", legacy)
	}
	for _, disabled := range []string{ssh.InsecureCipherRC4, ssh.InsecureCipherRC4128, ssh.InsecureCipherRC4256, "aes192-cbc", "aes256-cbc"} {
		if slices.Contains(cfg.Ciphers, disabled) {
			t.Fatalf("cipher list includes %s: %v", disabled, cfg.Ciphers)
		}
	}
}

func TestWaitIdleVRPRequiresReturn(t *testing.T) {
	s := &sshShellSession{
		lastActivity: time.Now().Add(-time.Hour),
		platform:     "vrp",
		promptHost:   "DR-25.Lassa",
	}
	s.buf.WriteString("interface Vbdif777\n ip address 10.119.25.1 255.255.255.0\n<DR-25.Lassa>\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := s.waitIdle(ctx, vrpConfigEndMarker)
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("waitIdle: got %v, want errWaitTimeout", err)
	}
}

func TestWaitIdleVRPReturnCompletes(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now().Add(-time.Hour), platform: "vrp", promptHost: "DR-25.Lassa"}
	s.buf.WriteString("interface Nve1\n source 10.110.10.25\n#\nreturn\n")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.waitIdle(ctx, vrpConfigEndMarker); err != nil {
		t.Fatal(err)
	}
}

func TestWaitIdleAlreadyQuiet(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now().Add(-idleWindow)}
	if err := s.waitIdle(context.Background(), nil); err != nil {
		t.Fatalf("waitIdle: %v", err)
	}
}

func TestWaitIdleCancel(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := s.waitIdle(ctx, nil)
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("waitIdle: got %v, want errWaitTimeout", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("waitIdle on cancelled ctx took %v, want immediate return", time.Since(start))
	}
}

func TestWaitIdleExitedWithPayloadReturnsEOF(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now(), done: make(chan struct{})}
	s.buf.WriteString("partial dump")
	s.markExited()
	err := s.waitIdle(context.Background(), nil)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("waitIdle: got %v, want io.EOF", err)
	}
}

func TestCLIPromptPatterns(t *testing.T) {
	cases := []struct {
		platform string
		line     string
		known    string
		want     string
		ok       bool
	}{
		{"sros", "A:admin@LU17-LAB-R4#", "", "LU17-LAB-R4", true},
		{"sros-md", "A:admin@LU17-LAB-R4# ", "", "LU17-LAB-R4", true},
		{"sros", "*A:admin@LU17-LAB-R4#", "", "LU17-LAB-R4", true},
		{"sros", "(ex)A:admin@LU17-LAB-R4#", "", "LU17-LAB-R4", true},
		{"sros", "*(ex)A:admin@LU17-LAB-R4#", "", "LU17-LAB-R4", true},
		{"sros", "*A:LU17-LAB-R4#", "", "LU17-LAB-R4", true},
		{"sros", "*A:LU17-LAB-R4>config>router#", "", "LU17-LAB-R4", true},
		{"sros", "[/]", "", "", false},
		{"sros", "*(ex)[/configure router \"Base\"]", "", "", false},
		{"sros", "System Name : LU17-LAB-R4", "", "", false},
		{"sros", "A:admin@OTHER#", "LU17-LAB-R4", "", false},
		{"vrp", "<LU17-LAB-R4>", "", "LU17-LAB-R4", true},
		{"vrp", "[~LU17-LAB-R4]", "", "LU17-LAB-R4", true},
		{"vrp", "[~LU17-LAB-R4-GigabitEthernet0/0/1]", "", "LU17-LAB-R4", true},
		{"vrp", "<LU17-LAB-R40>", "LU17-LAB-R4", "", false},
		{"vrp", "[~LU17-LAB-R4-GigabitEthernet0/0/1]", "LU17-LAB-R4", "LU17-LAB-R4", true},
		{"ciscosmb", "switch#", "", "switch", true},
		{"ciscosmb", "switch>", "", "switch", true},
		{"eos", "switch(config-if)#", "", "switch", true},
		{"ciscosmb", "other#", "switch", "", false},
		{"ios-xr", "RP/0/RP0/CPU0:LAB-R1#", "", "LAB-R1", true},
		{"ios-xr", "RP/0/RP0/CPU0:LAB-R1(config)#", "", "LAB-R1", true},
		{"ios-xr", "RP/0/RP0/CPU0:OTHER#", "LAB-R1", "", false},
		{"", "A:admin@LU17-LAB-R4#", "", "", false},
	}
	for _, tc := range cases {
		got, ok := matchCLIPrompt(tc.platform, tc.line, tc.known)
		if ok != tc.ok || got != tc.want {
			t.Errorf("matchCLIPrompt(%q, %q, %q) = %q, %v; want %q, %v", tc.platform, tc.line, tc.known, got, ok, tc.want, tc.ok)
		}
	}
}

func TestWaitIdleNokiaPromptReturnsImmediately(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now(), done: make(chan struct{})}
	s.setCLIPlatform("sros-md")
	s.buf.WriteString("\x1b[32mWelcome\x1b[0m\r\n[/]\r\nA:admin@LU17-LAB-R4# ")
	start := time.Now()
	if err := s.waitIdle(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("prompt wait took %s", time.Since(start))
	}
	if s.promptHost != "LU17-LAB-R4" {
		t.Fatalf("promptHost = %q", s.promptHost)
	}
}

func TestWaitIdleHostnameInOutputIsNotThePrompt(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now(), done: make(chan struct{})}
	s.setCLIPlatform("sros")
	s.promptHost = "LU17-LAB-R4"
	s.buf.WriteString("System Name            : LU17-LAB-R4\nTiMOS-B-21.10.R1\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := s.waitIdle(ctx, nil)
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("waitIdle: got %v, want timeout", err)
	}
}

func TestWaitIdlePromptAfterHostnameLine(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now(), done: make(chan struct{})}
	s.setCLIPlatform("sros")
	s.buf.WriteString("System Name            : LU17-LAB-R4\n[/]\nA:admin@LU17-LAB-R4# \n")
	if err := s.waitIdle(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.capture(), "System Name") {
		t.Fatalf("capture = %q", s.capture())
	}
}

func TestWaitIdlePipelineWaitsForEveryPrompt(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now(), done: make(chan struct{})}
	s.setCLIPlatform("sros")
	s.promptHost = "LU17-LAB-R4"
	s.buf.WriteString("out a\n[/]\nA:admin@LU17-LAB-R4#\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := s.waitIdleCount(ctx, nil, 3)
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("one prompt of three: got %v, want timeout", err)
	}

	s.buf.WriteString("out b\n[/]\nA:admin@LU17-LAB-R4#\nout c\n[/]\nA:admin@LU17-LAB-R4#\n")
	s.lastActivity = time.Now()
	start := time.Now()
	if err := s.waitIdleCount(context.Background(), nil, 3); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("three prompts took %s", time.Since(start))
	}
}

func TestWaitIdleVRPPromptAndView(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now(), done: make(chan struct{})}
	s.setCLIPlatform("vrp")
	s.buf.WriteString("<LAB-SW>")
	if err := s.waitIdle(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if s.promptHost != "LAB-SW" {
		t.Fatalf("promptHost = %q", s.promptHost)
	}
	s.resetCapture()
	s.buf.WriteString("interface GigabitEthernet0/0/1\n[~LAB-SW-GigabitEthernet0/0/1]")
	s.lastActivity = time.Now()
	start := time.Now()
	if err := s.waitIdle(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("view prompt took %s", time.Since(start))
	}
}

func useFakeSSHCLIPort(t *testing.T, f *fakeSSH) {
	t.Helper()
	_, port := f.addr()
	sshTestDialPort = port
	t.Cleanup(func() { sshTestDialPort = "" })
}

func TestNokiaPromptSkipsIdleWindow(t *testing.T) {
	f := startFakeSSH(t)
	useFakeSSHCLIPort(t, f)
	f.banner = "Welcome to SR OS\n[/]\nA:admin@LU17-LAB-R4# "
	f.replies["//environment no more"] = "[/]\nA:admin@LU17-LAB-R4# "
	f.replies["show version"] = "TiMOS\nSystem Name : LU17-LAB-R4\n[/]\nA:admin@LU17-LAB-R4# "
	p := f.param("sros-md")
	start := time.Now()
	out, err := sshRunCLI(context.Background(), p, []sshCmd{
		{Cmd: "//environment no more"},
		{Cmd: "show version"},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TiMOS") || !strings.Contains(out, "System Name") {
		t.Fatalf("output = %q", out)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("prompt detection fell back to idleWindow, took %s", elapsed)
	}
}

func TestVRPPromptSkipsIdleWindow(t *testing.T) {
	f := startFakeSSH(t)
	f.banner = "<LAB-SW>\n"
	f.replies["screen-length 0 temporary"] = "<LAB-SW>\n"
	f.replies["display version"] = "VRP version 1.0\n<LAB-SW>\n"
	ResetSSHPoolForTest()
	p := f.param("vrp")
	start := time.Now()
	out, err := sshRunCLI(context.Background(), p, []sshCmd{{Cmd: "display version"}})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "VRP version") {
		t.Fatalf("output = %q", out)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("prompt detection fell back to idleWindow, took %s", elapsed)
	}
}

func TestNokiaPipelinePromptCount(t *testing.T) {
	f := startFakeSSH(t)
	useFakeSSHCLIPort(t, f)
	f.banner = "[/]\nA:admin@LU17-LAB-R4# "
	for _, cmd := range []string{"show a", "show b", "show c"} {
		f.replies[cmd] = "body " + cmd + "\n[/]\nA:admin@LU17-LAB-R4#\n"
	}
	p := f.param("sros")
	start := time.Now()
	out, err := sshRunCLIPipeline(context.Background(), p, []string{"show a", "show b", "show c"}, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"show a", "show b", "show c"} {
		if !strings.Contains(out, "body "+cmd) {
			t.Fatalf("missing %s in %q", cmd, out)
		}
	}
	if elapsed > 2*time.Second {
		t.Fatalf("pipeline took %s", elapsed)
	}
}
