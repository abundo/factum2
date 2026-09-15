package certs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildLegoYAMLAndEnv(t *testing.T) {
	trueVal := true
	cfg := &Config{
		LegoStorage:             "/var/lib/lego",
		DefaultKeyType:          "EC256",
		DefaultEnableCommonName: false,
		Accounts: []Account{{
			Name: "letsencrypt", Email: "ops@example.com",
			Server:  "https://acme-v02.api.letsencrypt.org/directory",
			KeyType: "EC256", AcceptsTermsOfService: true,
		}},
		Challenges: []Challenge{{
			Name: "ns1", Provider: "rfc2136",
			RFC2136Nameserver: "192.0.2.53:53",
			RFC2136TSIGKey:    "acme-key",
			RFC2136TSIGSecret: "c2VjcmV0",
			RFC2136TSIGAlgo:   "hmac-sha256.",
			Resolvers:         []string{"192.0.2.53:53"},
		}},
		Certificates: []Cert{{
			Name: "web", Account: "letsencrypt", Challenge: "ns1",
			Domains:          []string{"example.com", "*.example.com"},
			EnableCommonName: &trueVal,
		}},
	}
	y, err := BuildLegoYAML(cfg, "/etc/factum2/lego/.env")
	if err != nil {
		t.Fatal(err)
	}
	s := string(y)
	for _, want := range []string{
		"storage:", "/var/lib/lego",
		"letsencrypt:", "ops@example.com",
		"provider: rfc2136",
		"envFile:",
		"example.com",
		"enableCommonName: true",
		"keyType: EC256",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("yaml missing %q:\n%s", want, s)
		}
	}
	env := BuildEnv(cfg.Challenges)
	for _, want := range []string{
		"RFC2136_NAMESERVER=192.0.2.53:53",
		"RFC2136_TSIG_KEY=acme-key",
		"RFC2136_TSIG_SECRET=c2VjcmV0",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %q:\n%s", want, env)
		}
	}
}

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		LegoYaml: filepath.Join(dir, ".lego.yaml"),
		EnvFile:  filepath.Join(dir, ".env"),
		Accounts: []Account{{Name: "a", Email: "a@example.com", AcceptsTermsOfService: true}},
		Challenges: []Challenge{{
			Name: "c", Provider: "rfc2136", RFC2136Nameserver: "127.0.0.1",
		}},
		Certificates: []Cert{{
			Name: "cert", Account: "a", Challenge: "c", Domains: []string{"ex.com"},
		}},
	}
	yp, ep, err := WriteFiles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(yp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ep); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(yp)
	if !strings.Contains(string(body), "rfc2136") {
		t.Fatalf("yaml: %s", body)
	}
}

func TestApplyConfigDefaults(t *testing.T) {
	cfg := &Config{}
	ApplyConfigDefaults(cfg)
	if cfg.LegoYaml != DefaultLegoYAML {
		t.Fatalf("yaml = %q", cfg.LegoYaml)
	}
	if cfg.EnvFile != DefaultEnvFile {
		t.Fatalf("env = %q", cfg.EnvFile)
	}
	if cfg.LegoBin != DefaultLegoBin {
		t.Fatalf("bin = %q", cfg.LegoBin)
	}
}
