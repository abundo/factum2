package certs

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	goyaml "github.com/goccy/go-yaml"
)

const (
	defaultACMEServer = "https://acme-v02.api.letsencrypt.org/directory"
	defaultAccountKey = "EC256"
	defaultDNSTimeout = 10
	maxImportNameLen  = 255
)

// legoServerCodes are the short names lego accepts in place of an ACME directory URL.
var legoServerCodes = map[string]string{
	"letsencrypt":         defaultACMEServer,
	"le":                  defaultACMEServer,
	"letsencrypt-staging": "https://acme-staging-v02.api.letsencrypt.org/directory",
	"staging":             "https://acme-staging-v02.api.letsencrypt.org/directory",
	"zerossl":             "https://acme.zerossl.com/v2/DV90",
	"buypass":             "https://api.buypass.com/acme/directory",
	"buypass-test":        "https://api.test4.buypass.no/acme/directory",
}

// Imported is a .lego.yaml (and optional dotenv) turned into factum rows.
// EnvApplied is true when dotenv text was supplied, including an empty file
// the caller still wants treated as the credential source.
type Imported struct {
	Accounts     []Account
	Challenges   []Challenge
	Certificates []Cert
	Skipped      []string
	EnvApplied   bool
}

type importDoc struct {
	Accounts     map[string]importAccount   `yaml:"accounts"`
	Challenges   map[string]importChallenge `yaml:"challenges"`
	Certificates map[string]importCert      `yaml:"certificates"`
	Servers      map[string]importServer    `yaml:"servers"`
}

type importServer struct {
	URL string `yaml:"url"`
}

type importAccount struct {
	Server                string     `yaml:"server"`
	Email                 string     `yaml:"email"`
	KeyType               string     `yaml:"keyType"`
	AcceptsTermsOfService bool       `yaml:"acceptsTermsOfService"`
	EAB                   *importEAB `yaml:"eab"`
}

type importEAB struct {
	KID     string `yaml:"kid"`
	HMACKey string `yaml:"hmacKey"`
}

type importChallenge struct {
	DNS        *importDNS `yaml:"dns"`
	HTTP       *struct{}  `yaml:"http"`
	TLS        *struct{}  `yaml:"tls"`
	DNSPersist *struct{}  `yaml:"dnsPersist"`
}

type importDNS struct {
	Provider    string             `yaml:"provider"`
	DNSTimeout  int                `yaml:"dnsTimeout"`
	Resolvers   []string           `yaml:"resolvers"`
	Propagation *importPropagation `yaml:"propagation"`
}

type importPropagation struct {
	DisableAuthoritativeNameservers bool `yaml:"disableAuthoritativeNameservers"`
	DisableRecursiveNameservers     bool `yaml:"disableRecursiveNameservers"`
	Wait                            any  `yaml:"wait"`
}

type importCert struct {
	Challenge        string   `yaml:"challenge"`
	Account          string   `yaml:"account"`
	KeyType          string   `yaml:"keyType"`
	Domains          []string `yaml:"domains"`
	EnableCommonName *bool    `yaml:"enableCommonName"`
	CSR              string   `yaml:"csr"`
}

type parsedChallenge struct {
	ch     Challenge
	ok     bool
	reason string
}

// ParseLegoFile reads a lego configuration file. Only RFC2136 DNS-01
// challenges are imported. envText, when non-empty after trimming, supplies
// RFC2136_* credentials for every imported challenge. Other env lines are
// kept as extra env. Rows that already exist are the caller's concern;
// this function only parses.
func ParseLegoFile(yamlText, envText string) (Imported, error) {
	var doc importDoc
	if err := goyaml.Unmarshal([]byte(yamlText), &doc); err != nil {
		return Imported{}, fmt.Errorf("invalid lego yaml: %w", err)
	}
	if len(doc.Accounts) == 0 && len(doc.Challenges) == 0 && len(doc.Certificates) == 0 {
		return Imported{}, fmt.Errorf("lego file has no accounts, challenges, or certificates")
	}

	servers := map[string]string{}
	for name, srv := range doc.Servers {
		servers[strings.TrimSpace(name)] = strings.TrimSpace(srv.URL)
	}

	accounts := map[string]Account{}
	for name, raw := range doc.Accounts {
		name, err := cleanImportName(name, "account")
		if err != nil {
			return Imported{}, err
		}
		acc := Account{
			Name:                  name,
			Email:                 strings.TrimSpace(raw.Email),
			Server:                resolveACMEServer(raw.Server, servers),
			KeyType:               strings.TrimSpace(raw.KeyType),
			AcceptsTermsOfService: raw.AcceptsTermsOfService,
		}
		if acc.KeyType == "" {
			acc.KeyType = defaultAccountKey
		}
		if raw.EAB != nil {
			acc.EABKID = strings.TrimSpace(raw.EAB.KID)
			acc.EABHMACKey = strings.TrimSpace(raw.EAB.HMACKey)
		}
		accounts[name] = acc
	}

	challenges := map[string]parsedChallenge{}
	for name, raw := range doc.Challenges {
		name, err := cleanImportName(name, "challenge")
		if err != nil {
			return Imported{}, err
		}
		parsed, err := parseImportChallenge(name, raw)
		if err != nil {
			return Imported{}, err
		}
		challenges[name] = parsed
	}

	envApplied := strings.TrimSpace(envText) != ""
	var envVals map[string]string
	var extraEnv string
	if envApplied {
		envVals, extraEnv = parseLegoEnv(envText)
	}

	certs := map[string]Cert{}
	for name, raw := range doc.Certificates {
		name, err := cleanImportName(name, "certificate")
		if err != nil {
			return Imported{}, err
		}
		cert, err := parseImportCert(name, raw, accounts, challenges)
		if err != nil {
			return Imported{}, err
		}
		certs[name] = cert
	}

	// Challenges a certificate points at must be RFC2136. Others are skipped
	// when nothing references them.
	referenced := map[string]string{}
	for _, cert := range certs {
		referenced[cert.Challenge] = cert.Name
	}
	var skipped []string
	importedChallenges := map[string]Challenge{}
	for name, parsed := range challenges {
		if !parsed.ok {
			if certName, used := referenced[name]; used {
				return Imported{}, fmt.Errorf("certificate %s uses challenge %s (%s)", certName, name, parsed.reason)
			}
			skipped = append(skipped, fmt.Sprintf("challenge %s was skipped (%s)", name, parsed.reason))
			continue
		}
		ch := parsed.ch
		if envApplied {
			applyRFC2136Env(&ch, envVals, extraEnv)
		}
		importedChallenges[name] = ch
	}

	if len(accounts) == 0 && len(importedChallenges) == 0 && len(certs) == 0 {
		if len(skipped) > 0 {
			sort.Strings(skipped)
			return Imported{}, fmt.Errorf("nothing to import: %s", strings.Join(skipped, "; "))
		}
		return Imported{}, fmt.Errorf("nothing to import")
	}

	out := Imported{Skipped: skipped, EnvApplied: envApplied}
	for _, name := range sortedKeys(accounts) {
		out.Accounts = append(out.Accounts, accounts[name])
	}
	for _, name := range sortedKeys(importedChallenges) {
		out.Challenges = append(out.Challenges, importedChallenges[name])
	}
	for _, name := range sortedKeys(certs) {
		out.Certificates = append(out.Certificates, certs[name])
	}
	sort.Strings(out.Skipped)
	return out, nil
}

func parseImportChallenge(name string, raw importChallenge) (parsedChallenge, error) {
	if raw.DNS == nil {
		reason := "not a DNS-01 challenge"
		switch {
		case raw.HTTP != nil:
			reason = "HTTP-01 is not supported"
		case raw.TLS != nil:
			reason = "TLS-ALPN-01 is not supported"
		case raw.DNSPersist != nil:
			reason = "DNS-PERSIST-01 is not supported"
		}
		return parsedChallenge{reason: reason}, nil
	}
	provider := strings.ToLower(strings.TrimSpace(raw.DNS.Provider))
	if provider == "" {
		return parsedChallenge{}, fmt.Errorf("challenge %s: dns provider is required", name)
	}
	if provider != "rfc2136" {
		return parsedChallenge{reason: "provider " + provider + " is not rfc2136"}, nil
	}
	timeout := raw.DNS.DNSTimeout
	if timeout == 0 {
		timeout = defaultDNSTimeout
	}
	if timeout < 0 {
		return parsedChallenge{}, fmt.Errorf("challenge %s: dns timeout must be positive", name)
	}
	ch := Challenge{
		Name:       name,
		Kind:       "dns-01",
		Provider:   "rfc2136",
		DNSTimeout: timeout,
		Resolvers:  cleanLines(raw.DNS.Resolvers),
	}
	if raw.DNS.Propagation != nil {
		ch.DisableAuthNS = raw.DNS.Propagation.DisableAuthoritativeNameservers
		ch.DisableRecursiveNS = raw.DNS.Propagation.DisableRecursiveNameservers
		ch.PropagationWait = yamlScalar(raw.DNS.Propagation.Wait)
	}
	return parsedChallenge{ch: ch, ok: true}, nil
}

func parseImportCert(name string, raw importCert, accounts map[string]Account, challenges map[string]parsedChallenge) (Cert, error) {
	domains := cleanLines(raw.Domains)
	if len(domains) == 0 {
		if strings.TrimSpace(raw.CSR) != "" {
			return Cert{}, fmt.Errorf("certificate %s uses a CSR file, which cannot be imported", name)
		}
		return Cert{}, fmt.Errorf("certificate %s has no domains", name)
	}
	account, err := resolveAccountName(raw.Account, accounts)
	if err != nil {
		return Cert{}, fmt.Errorf("certificate %s: %w", name, err)
	}
	challenge, err := resolveChallengeName(raw.Challenge, challenges)
	if err != nil {
		return Cert{}, fmt.Errorf("certificate %s: %w", name, err)
	}
	return Cert{
		Name:             name,
		Account:          account,
		Challenge:        challenge,
		KeyType:          strings.TrimSpace(raw.KeyType),
		EnableCommonName: raw.EnableCommonName,
		Domains:          domains,
	}, nil
}

func resolveAccountName(explicit string, accounts map[string]Account) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		// A name that is not in this file can still match a row already stored.
		return explicit, nil
	}
	switch len(accounts) {
	case 0:
		accounts["default"] = Account{
			Name:                  "default",
			Server:                defaultACMEServer,
			KeyType:               defaultAccountKey,
			AcceptsTermsOfService: true,
		}
		return "default", nil
	case 1:
		for name := range accounts {
			return name, nil
		}
	}
	return "", fmt.Errorf("account is required when more than one account is defined")
}

func resolveChallengeName(explicit string, challenges map[string]parsedChallenge) (string, error) {
	explicit = strings.TrimSpace(explicit)
	switch explicit {
	case "http-01":
		return "", fmt.Errorf("HTTP-01 challenges are not supported")
	case "tls-alpn-01":
		return "", fmt.Errorf("TLS-ALPN-01 challenges are not supported")
	}
	if explicit != "" {
		// A name that is not in this file can still match a row already stored.
		return explicit, nil
	}
	if len(challenges) == 1 {
		for name := range challenges {
			return name, nil
		}
	}
	if len(challenges) == 0 {
		return "", fmt.Errorf("challenge is required")
	}
	return "", fmt.Errorf("challenge is required when more than one challenge is defined")
}

func resolveACMEServer(raw string, servers map[string]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultACMEServer
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	if url := strings.TrimSpace(servers[raw]); url != "" {
		return url
	}
	if url, ok := legoServerCodes[strings.ToLower(raw)]; ok {
		return url
	}
	return raw
}

func applyRFC2136Env(ch *Challenge, env map[string]string, extra string) {
	ch.RFC2136Nameserver = env["RFC2136_NAMESERVER"]
	ch.RFC2136TSIGAlgo = env["RFC2136_TSIG_ALGORITHM"]
	ch.RFC2136TSIGKey = env["RFC2136_TSIG_KEY"]
	ch.RFC2136TSIGSecret = env["RFC2136_TSIG_SECRET"]
	ch.RFC2136TSIGFile = env["RFC2136_TSIG_FILE"]
	ch.RFC2136PropTimeout = env["RFC2136_PROPAGATION_TIMEOUT"]
	ch.RFC2136PollInterval = env["RFC2136_POLLING_INTERVAL"]
	if raw := env["RFC2136_TTL"]; raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			ch.RFC2136TTL = n
		}
	}
	ch.ExtraEnv = extra
}

func parseLegoEnv(body string) (map[string]string, string) {
	knownKeys := map[string]struct{}{
		"RFC2136_NAMESERVER":          {},
		"RFC2136_TSIG_ALGORITHM":      {},
		"RFC2136_TSIG_KEY":            {},
		"RFC2136_TSIG_SECRET":         {},
		"RFC2136_TSIG_FILE":           {},
		"RFC2136_TTL":                 {},
		"RFC2136_PROPAGATION_TIMEOUT": {},
		"RFC2136_POLLING_INTERVAL":    {},
	}
	vals := map[string]string{}
	var extra []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			extra = append(extra, line)
			continue
		}
		key = strings.TrimSpace(key)
		value = unquoteEnv(strings.TrimSpace(value))
		if _, known := knownKeys[key]; known {
			vals[key] = value
			continue
		}
		extra = append(extra, key+"="+value)
	}
	return vals, strings.Join(extra, "\n")
}

func unquoteEnv(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func cleanImportName(name, kind string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%s name is empty", kind)
	}
	if len(name) > maxImportNameLen {
		return "", fmt.Errorf("%s name %s is too long", kind, name)
	}
	if strings.ContainsAny(name, "\r\n") {
		return "", fmt.Errorf("%s name %s is invalid", kind, name)
	}
	return name, nil
}

func cleanLines(lines []string) []string {
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func yamlScalar(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
