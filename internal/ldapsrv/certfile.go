package ldapsrv

import (
	"fmt"
	"strings"

	"golang.org/x/net/idna"
)

// LegoCertStem is the file name lego uses for a certificate domain, without
// an extension. It matches lego's sanitizedDomain: ":" becomes "-", "*"
// becomes "_", then the name is converted to ASCII. The result is one
// path segment.
func LegoCertStem(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", fmt.Errorf("certificate domain is empty")
	}
	safe, err := idna.ToASCII(strings.NewReplacer(":", "-", "*", "_").Replace(domain))
	if err != nil {
		return "", fmt.Errorf("certificate domain %q: %w", domain, err)
	}
	if safe == "" || safe == "." || safe == ".." || strings.ContainsAny(safe, `/\`) {
		return "", fmt.Errorf("certificate domain %q is not a file name", domain)
	}
	return safe, nil
}
