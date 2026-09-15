package certs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goyaml "github.com/goccy/go-yaml"
)

type legoFile struct {
	Storage      string                   `yaml:"storage,omitempty"`
	Accounts     map[string]legoAccount   `yaml:"accounts,omitempty"`
	Challenges   map[string]legoChallenge `yaml:"challenges,omitempty"`
	Certificates map[string]legoCert      `yaml:"certificates,omitempty"`
}

type legoAccount struct {
	Server                string   `yaml:"server,omitempty"`
	Email                 string   `yaml:"email,omitempty"`
	KeyType               string   `yaml:"keyType,omitempty"`
	AcceptsTermsOfService bool     `yaml:"acceptsTermsOfService,omitempty"`
	EAB                   *legoEAB `yaml:"eab,omitempty"`
}

type legoEAB struct {
	KID     string `yaml:"kid"`
	HMACKey string `yaml:"hmacKey"`
}

type legoChallenge struct {
	DNS *legoDNS `yaml:"dns,omitempty"`
}

type legoDNS struct {
	Provider    string           `yaml:"provider"`
	EnvFile     string           `yaml:"envFile,omitempty"`
	DNSTimeout  int              `yaml:"dnsTimeout,omitempty"`
	Resolvers   []string         `yaml:"resolvers,omitempty"`
	Propagation *legoPropagation `yaml:"propagation,omitempty"`
}

type legoPropagation struct {
	DisableAuthoritativeNameservers bool   `yaml:"disableAuthoritativeNameservers,omitempty"`
	DisableRecursiveNameservers     bool   `yaml:"disableRecursiveNameservers,omitempty"`
	Wait                            string `yaml:"wait,omitempty"`
}

type legoCert struct {
	Challenge        string   `yaml:"challenge"`
	Account          string   `yaml:"account,omitempty"`
	KeyType          string   `yaml:"keyType,omitempty"`
	Domains          []string `yaml:"domains"`
	EnableCommonName *bool    `yaml:"enableCommonName,omitempty"`
}

func BuildLegoYAML(cfg *Config, envFile string) ([]byte, error) {
	out := legoFile{
		Storage:      strings.TrimSpace(cfg.LegoStorage),
		Accounts:     map[string]legoAccount{},
		Challenges:   map[string]legoChallenge{},
		Certificates: map[string]legoCert{},
	}
	for _, a := range cfg.Accounts {
		acc := legoAccount{
			Server:                strings.TrimSpace(a.Server),
			Email:                 strings.TrimSpace(a.Email),
			KeyType:               strings.TrimSpace(a.KeyType),
			AcceptsTermsOfService: a.AcceptsTermsOfService,
		}
		if strings.TrimSpace(a.EABKID) != "" || strings.TrimSpace(a.EABHMACKey) != "" {
			acc.EAB = &legoEAB{KID: a.EABKID, HMACKey: a.EABHMACKey}
		}
		out.Accounts[a.Name] = acc
	}
	for _, ch := range cfg.Challenges {
		provider := strings.TrimSpace(ch.Provider)
		if provider == "" {
			provider = "rfc2136"
		}
		dns := &legoDNS{
			Provider:   provider,
			EnvFile:    envFile,
			DNSTimeout: ch.DNSTimeout,
			Resolvers:  ch.Resolvers,
		}
		if ch.DisableAuthNS || ch.DisableRecursiveNS || strings.TrimSpace(ch.PropagationWait) != "" {
			dns.Propagation = &legoPropagation{
				DisableAuthoritativeNameservers: ch.DisableAuthNS,
				DisableRecursiveNameservers:     ch.DisableRecursiveNS,
				Wait:                            strings.TrimSpace(ch.PropagationWait),
			}
		}
		out.Challenges[ch.Name] = legoChallenge{DNS: dns}
	}
	for _, c := range cfg.Certificates {
		keyType := strings.TrimSpace(c.KeyType)
		if keyType == "" {
			keyType = strings.TrimSpace(cfg.DefaultKeyType)
		}
		enableCN := c.EnableCommonName
		if enableCN == nil && cfg.DefaultEnableCommonName {
			v := true
			enableCN = &v
		}
		out.Certificates[c.Name] = legoCert{
			Challenge:        c.Challenge,
			Account:          c.Account,
			KeyType:          keyType,
			Domains:          c.Domains,
			EnableCommonName: enableCN,
		}
	}
	return goyaml.Marshal(out)
}

func BuildEnv(challenges []Challenge) string {
	var b strings.Builder
	for _, ch := range challenges {
		if len(challenges) > 1 {
			fmt.Fprintf(&b, "# challenge %s\n", ch.Name)
		}
		set := func(k, v string) {
			v = strings.TrimSpace(v)
			if v == "" {
				return
			}
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
		set("RFC2136_NAMESERVER", ch.RFC2136Nameserver)
		set("RFC2136_TSIG_ALGORITHM", ch.RFC2136TSIGAlgo)
		set("RFC2136_TSIG_KEY", ch.RFC2136TSIGKey)
		set("RFC2136_TSIG_SECRET", ch.RFC2136TSIGSecret)
		set("RFC2136_TSIG_FILE", ch.RFC2136TSIGFile)
		if ch.RFC2136TTL > 0 {
			fmt.Fprintf(&b, "RFC2136_TTL=%d\n", ch.RFC2136TTL)
		}
		set("RFC2136_PROPAGATION_TIMEOUT", ch.RFC2136PropTimeout)
		set("RFC2136_POLLING_INTERVAL", ch.RFC2136PollInterval)
		for _, line := range strings.Split(ch.ExtraEnv, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fmt.Fprintln(&b, line)
		}
	}
	return b.String()
}

func WriteFiles(cfg *Config) (yamlPath, envPath string, err error) {
	ApplyConfigDefaults(cfg)
	yamlPath = strings.TrimSpace(cfg.LegoYaml)
	envPath = strings.TrimSpace(cfg.EnvFile)
	absEnv, err := filepath.Abs(envPath)
	if err != nil {
		return "", "", err
	}
	body, err := BuildLegoYAML(cfg, absEnv)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(yamlPath, body, 0o600); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(envPath, []byte(BuildEnv(cfg.Challenges)), 0o600); err != nil {
		return "", "", err
	}
	return yamlPath, envPath, nil
}
