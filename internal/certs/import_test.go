package certs

import (
	"strings"
	"testing"
)

func TestParseLegoFileRoundTrip(t *testing.T) {
	trueVal := true
	cfg := &Config{
		LegoStorage:    "/var/lib/lego",
		DefaultKeyType: "EC256",
		Accounts: []Account{{
			Name: "letsencrypt", Email: "ops@example.com",
			Server: defaultACMEServer, KeyType: "EC256", AcceptsTermsOfService: true,
		}},
		Challenges: []Challenge{{
			Name: "ns1", Provider: "rfc2136", DNSTimeout: 10,
			Resolvers:         []string{"192.0.2.53:53"},
			RFC2136Nameserver: "192.0.2.53:53",
			RFC2136TSIGKey:    "acme-key",
			RFC2136TSIGSecret: "c2VjcmV0",
			RFC2136TSIGAlgo:   "hmac-sha256.",
		}},
		Certificates: []Cert{{
			Name: "web", Account: "letsencrypt", Challenge: "ns1",
			KeyType: "EC256", Domains: []string{"example.com", "*.example.com"},
			EnableCommonName: &trueVal,
		}},
	}
	y, err := BuildLegoYAML(cfg, "/etc/factum2/lego/.env")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseLegoFile(string(y), BuildEnv(cfg.Challenges))
	if err != nil {
		t.Fatal(err)
	}
	if !got.EnvApplied {
		t.Fatal("env was not applied")
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Email != "ops@example.com" {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
	if len(got.Challenges) != 1 {
		t.Fatalf("challenges = %+v", got.Challenges)
	}
	ch := got.Challenges[0]
	if ch.Provider != "rfc2136" || ch.RFC2136Nameserver != "192.0.2.53:53" || ch.RFC2136TSIGSecret != "c2VjcmV0" {
		t.Fatalf("challenge = %+v", ch)
	}
	if len(ch.Resolvers) != 1 || ch.Resolvers[0] != "192.0.2.53:53" {
		t.Fatalf("resolvers = %#v", ch.Resolvers)
	}
	if len(got.Certificates) != 1 || got.Certificates[0].Name != "web" {
		t.Fatalf("certs = %+v", got.Certificates)
	}
	if got.Certificates[0].EnableCommonName == nil || !*got.Certificates[0].EnableCommonName {
		t.Fatalf("enable CN = %v", got.Certificates[0].EnableCommonName)
	}
	if len(got.Certificates[0].Domains) != 2 {
		t.Fatalf("domains = %#v", got.Certificates[0].Domains)
	}
}

func TestParseLegoFileDefaultsAndSkip(t *testing.T) {
	body := `
defaults: &defaults
  account: custom
  challenge: ns
  keyType: EC384
accounts:
  staging:
    server: letsencrypt-staging
  custom:
    server: pebble
    keyType: RSA2048
    acceptsTermsOfService: true
    eab:
      kid: kid-1
      hmacKey: hmac-1
servers:
  pebble:
    url: https://pebble.example/dir
challenges:
  ns:
    dns:
      provider: RFC2136
      propagation:
        wait: 5
        disableRecursiveNameservers: true
  cf:
    dns:
      provider: cloudflare
certificates:
  one:
    domains:
      - example.com
    account: staging
    challenge: ns
  two:
    <<: *defaults
    domains:
      - other.example
`
	got, err := ParseLegoFile(body, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.EnvApplied {
		t.Fatal("env applied from empty text")
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
	byName := map[string]Account{}
	for _, a := range got.Accounts {
		byName[a.Name] = a
	}
	if byName["staging"].Server != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Fatalf("staging server = %s", byName["staging"].Server)
	}
	if byName["staging"].KeyType != defaultAccountKey {
		t.Fatalf("staging key = %s", byName["staging"].KeyType)
	}
	if byName["custom"].Server != "https://pebble.example/dir" || byName["custom"].EABKID != "kid-1" {
		t.Fatalf("custom = %+v", byName["custom"])
	}
	if len(got.Challenges) != 1 || got.Challenges[0].Name != "ns" {
		t.Fatalf("challenges = %+v", got.Challenges)
	}
	if got.Challenges[0].DNSTimeout != defaultDNSTimeout || !got.Challenges[0].DisableRecursiveNS {
		t.Fatalf("challenge flags = %+v", got.Challenges[0])
	}
	if got.Challenges[0].PropagationWait != "5" {
		t.Fatalf("wait = %q", got.Challenges[0].PropagationWait)
	}
	if len(got.Skipped) != 1 || !strings.Contains(got.Skipped[0], "cloudflare") {
		t.Fatalf("skipped = %#v", got.Skipped)
	}
	if len(got.Certificates) != 2 {
		t.Fatalf("certs = %+v", got.Certificates)
	}
	if got.Certificates[1].Name != "two" || got.Certificates[1].Account != "custom" || got.Certificates[1].KeyType != "EC384" {
		t.Fatalf("merged cert = %+v", got.Certificates[1])
	}
}

func TestParseLegoFileRejectsUnsupportedUse(t *testing.T) {
	_, err := ParseLegoFile(`
challenges:
  cf:
    dns:
      provider: cloudflare
certificates:
  web:
    challenge: cf
    domains: [example.com]
`, "")
	if err == nil || !strings.Contains(err.Error(), "cloudflare") {
		t.Fatalf("err = %v", err)
	}

	_, err = ParseLegoFile(`
certificates:
  web:
    challenge: http-01
    domains: [example.com]
`, "")
	if err == nil || !strings.Contains(err.Error(), "HTTP-01") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseLegoEnvExtra(t *testing.T) {
	got, err := ParseLegoFile(`
accounts:
  a:
    email: a@example.com
challenges:
  ns:
    dns:
      provider: rfc2136
certificates:
  web:
    domains: [example.com]
`, "export RFC2136_NAMESERVER=\"ns.example:53\"\nRFC2136_TTL=30\nOTHER=1\n")
	if err != nil {
		t.Fatal(err)
	}
	ch := got.Challenges[0]
	if ch.RFC2136Nameserver != "ns.example:53" || ch.RFC2136TTL != 30 || ch.ExtraEnv != "OTHER=1" {
		t.Fatalf("challenge = %+v", ch)
	}
	if got.Certificates[0].Account != "a" || got.Certificates[0].Challenge != "ns" {
		t.Fatalf("cert = %+v", got.Certificates[0])
	}
}
