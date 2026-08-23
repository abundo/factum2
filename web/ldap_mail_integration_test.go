//go:build integration

package web

// Integration tests for internal/ldapauth and internal/mail, run against a
// real OpenLDAP server and mailpit SMTP catcher (testdata/itest) instead of
// the sqlite-only fakes the rest of this package's tests use - neither LDAP
// nor mail had any test coverage before this:
//
//	make itest-up               # start postgres+openldap+mailpit
//	make test-integration-web   # run these against it
//	make itest-down
//
// Point them at something else with the environment:
//
//	FACTUM_TEST_LDAP_HOST     default "localhost"
//	FACTUM_TEST_LDAP_PORT     default 3389
//	FACTUM_TEST_SMTP_HOST     default "localhost"
//	FACTUM_TEST_SMTP_PORT     default 11025
//	FACTUM_TEST_MAILPIT_API   default "http://localhost:18025" - mailpit's
//	                          REST API, used to assert a sent mail arrived
//
// The seed user/group (uid=testuser, cn=admins) come from
// testdata/itest/ldap/bootstrap.ldif - keep the two in sync if you change
// one.

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/internal/mail"
	"github.com/abundo/factum2/internal/util"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envPortOr(key string, def uint16) uint16 {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return def
	}
	return uint16(n)
}

// skipUnlessReachable skips the test rather than failing it when the itest
// stack isn't up - same shape as the driver integration tests skipping when
// FACTUM_TEST_EOS_HOST is unset, so `go test -tags integration ./...` from
// a dev machine without the stack running doesn't look like a real failure.
func skipUnlessReachable(t *testing.T, host string, port uint16) {
	t.Helper()
	addr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Skipf("%s not reachable (%v) - run `make itest-up` first, see this file's header comment", addr, err)
	}
	conn.Close()
}

func testLDAPConfig() ldapauth.Config {
	return ldapauth.Config{
		Host:            envOr("FACTUM_TEST_LDAP_HOST", "localhost"),
		Port:            envPortOr("FACTUM_TEST_LDAP_PORT", 3389),
		TLSMode:         "none",
		ServerType:      "generic",
		BindDN:          "cn=admin,dc=factum,dc=test",
		BindPassword:    "admin",
		BaseDN:          "dc=factum,dc=test",
		UserFilter:      "(uid=%s)",
		AttrUsername:    "uid",
		AttrEmail:       "mail",
		AttrDisplayName: "displayName",
		AttrMobile:      "mobile",
		AttrGroups:      "memberOf",
	}
}

func TestIntegrationLDAPAuthenticate(t *testing.T) {
	cfg := testLDAPConfig()
	skipUnlessReachable(t, cfg.Host, cfg.Port)

	ok, info, err := ldapauth.Authenticate(cfg, "testuser", "testpass123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok {
		t.Fatal("Authenticate: expected success, got bad credentials")
	}
	if info.Email != "testuser@factum.test" {
		t.Errorf("Email = %q, want testuser@factum.test", info.Email)
	}
	if info.DisplayName != "Test User" {
		t.Errorf("DisplayName = %q, want %q", info.DisplayName, "Test User")
	}

	ok, _, err = ldapauth.Authenticate(cfg, "testuser", "wrong-password")
	if err != nil {
		t.Fatalf("Authenticate with wrong password: unexpected error %v", err)
	}
	if ok {
		t.Fatal("Authenticate with wrong password: expected failure, got success")
	}
}

func TestIntegrationLDAPAuthenticateFailover(t *testing.T) {
	cfg := testLDAPConfig()
	skipUnlessReachable(t, cfg.Host, cfg.Port)

	cfg.Host2 = cfg.Host
	cfg.Port2 = cfg.Port
	cfg.Host = "127.0.0.1"
	cfg.Port = 1 // nothing listens here - forces the Host2 path

	ok, info, err := ldapauth.Authenticate(cfg, "testuser", "testpass123")
	if err != nil {
		t.Fatalf("Authenticate via secondary: %v", err)
	}
	if !ok {
		t.Fatal("Authenticate via secondary: expected success, got bad credentials")
	}
	if info.Email != "testuser@factum.test" {
		t.Errorf("Email = %q, want testuser@factum.test", info.Email)
	}
}

func TestIntegrationLDAPLookupByEmail(t *testing.T) {
	cfg := testLDAPConfig()
	skipUnlessReachable(t, cfg.Host, cfg.Port)

	info, err := ldapauth.LookupByEmail(cfg, "testuser@factum.test")
	if err != nil {
		t.Fatalf("LookupByEmail: %v", err)
	}
	if info == nil {
		t.Fatal("LookupByEmail: expected a match, got nil")
	}
	if info.Username != "testuser" {
		t.Errorf("Username = %q, want testuser", info.Username)
	}
}

func TestIntegrationMailSend(t *testing.T) {
	smtpHost := envOr("FACTUM_TEST_SMTP_HOST", "localhost")
	smtpPort := envPortOr("FACTUM_TEST_SMTP_PORT", 11025)
	skipUnlessReachable(t, smtpHost, smtpPort)

	// Unique recipient per run: mailpit's search API matches substrings, so
	// reusing a fixed address would let a message left over from a previous
	// run satisfy this assertion for the wrong reason.
	to := fmt.Sprintf("itest-%d@factum.test", time.Now().UnixNano())
	smtp := util.CommonConfig{SmtpHost: smtpHost, SmtpPort: smtpPort, SmtpTLSMode: "none"}
	if err := mail.Send(smtp, "factum@factum.test", to, "integration test", "<p>hello from factum2</p>"); err != nil {
		t.Fatalf("mail.Send: %v", err)
	}

	subject := waitForMailpitSubject(t, envOr("FACTUM_TEST_MAILPIT_API", "http://localhost:18025"), to)
	if subject != "integration test" {
		t.Errorf("Subject = %q, want %q", subject, "integration test")
	}
}

type mailpitSearchResult struct {
	Messages []struct {
		Subject string `json:"Subject"`
	} `json:"messages"`
}

// waitForMailpitSubject polls mailpit's search API for a message to `to`.
// SMTP delivery into the container is effectively synchronous, but a short
// retry loop avoids flaking on a slow host instead of a single request
// racing mail.Send's return.
func waitForMailpitSubject(t *testing.T, apiBase, to string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/search?query=%s", apiBase, "to%3A"+to)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			var result mailpitSearchResult
			decErr := json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if decErr == nil && len(result.Messages) > 0 {
				return result.Messages[0].Subject
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no message to %s arrived at mailpit within 5s (%s)", to, url)
	return ""
}
