package prometheus

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCompileIgnoreInterfaces(t *testing.T) {
	got, err := CompileIgnoreInterfaces("\n  Vlan.* \r\n\nNull.*\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "(?:Vlan.*)|(?:Null.*)" {
		t.Fatalf("pattern = %q", got)
	}
	re := regexp.MustCompile("^(?:" + got + ")$")
	if !re.MatchString("Vlan10") || !re.MatchString("Null0") || re.MatchString("Ethernet1") || re.MatchString("") {
		t.Fatalf("match behavior for %q", got)
	}

	if _, err := CompileIgnoreInterfaces("  \n"); err != nil {
		t.Fatal(err)
	}
	if got, err := CompileIgnoreInterfaces("  \n"); err != nil || got != "" {
		t.Fatalf("empty = %q %v", got, err)
	}
	if _, err := CompileIgnoreInterfaces(".*"); err == nil {
		t.Fatal("pattern that matches an empty name should be rejected")
	}
	if _, err := CompileIgnoreInterfaces("["); err == nil {
		t.Fatal("invalid regexp should be rejected")
	}
}

func TestNeverMatchDropsNothing(t *testing.T) {
	re := regexp.MustCompile("^(?:" + neverMatch + ")$")
	for _, name := range []string{"", "Ethernet1", "a^", "Vlan10"} {
		if re.MatchString(name) {
			t.Fatalf("%q matched %q", neverMatch, name)
		}
	}
}

func TestReplaceIgnoreBlock(t *testing.T) {
	body := []byte("scrape_configs:\n  - job_name: snmp\n    metric_relabel_configs:\n      # factum-if-ignore:begin\n      - source_labels: [ifName]\n        regex: \"a^\"\n        action: drop\n      # factum-if-ignore:end\n    other: true\n")
	updated, changed, err := replaceIgnoreBlock(body, "(?:Vlan.*)")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected a change")
	}
	s := string(updated)
	if !strings.Contains(s, `regex: "(?:Vlan.*)"`) {
		t.Fatalf("block = %s", s)
	}
	if !strings.Contains(s, "source_labels: [ifAlias]") || !strings.Contains(s, "other: true") {
		t.Fatalf("block = %s", s)
	}
	again, changed, err := replaceIgnoreBlock(updated, "(?:Vlan.*)")
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(again) != s {
		t.Fatal("second replace should be identical")
	}
	cleared, changed, err := replaceIgnoreBlock(updated, "")
	if err != nil || !changed {
		t.Fatalf("clear changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(cleared), `regex: "a^"`) {
		t.Fatalf("cleared = %s", cleared)
	}
}

func TestInstallIgnoreInterfaces(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(dest, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "runtime.yml")
	initial := "job:\n  # factum-if-ignore:begin\n  - source_labels: [ifName]\n    regex: \"a^\"\n    action: drop\n  - source_labels: [ifDescr]\n    regex: \"a^\"\n    action: drop\n  - source_labels: [ifAlias]\n    regex: \"a^\"\n    action: drop\n  # factum-if-ignore:end\n"
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	path, changed, err := InstallIgnoreInterfaces(dest, "Loopback.*")
	if err != nil {
		t.Fatal(err)
	}
	if path != cfg || !changed {
		t.Fatalf("path=%s changed=%v", path, changed)
	}
	_, changed, err = InstallIgnoreInterfaces(dest, "Loopback.*")
	if err != nil || changed {
		t.Fatalf("second changed=%v err=%v", changed, err)
	}

	parent := t.TempDir()
	sub := filepath.Join(parent, "file_sd")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	parentCfg := filepath.Join(parent, "prometheus.yml")
	if err := os.WriteFile(parentCfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	path, changed, err = InstallIgnoreInterfaces(filepath.Join(sub, "targets.json"), "Null.*")
	if err != nil {
		t.Fatal(err)
	}
	if path != parentCfg || !changed {
		t.Fatalf("parent path=%s changed=%v", path, changed)
	}
}

func TestShippedIgnoreBlock(t *testing.T) {
	for _, path := range []string{
		"../../dev/prometheus/prometheus.yml",
		"../../examples/prometheus/prometheus/prometheus.yml",
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		updated, changed, err := replaceIgnoreBlock(body, "")
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if changed {
			t.Fatalf("%s: empty ignore list would rewrite the shipped block:\n%s", path, updated)
		}
	}
}

func TestInstallIgnoreInterfacesMissing(t *testing.T) {
	dir := t.TempDir()
	_, _, err := InstallIgnoreInterfaces(filepath.Join(dir, "targets.json"), "Vlan.*")
	if !errors.Is(err, ErrNoIgnoreConfig) {
		t.Fatalf("err = %v", err)
	}
}
