package ldapsrv

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/go-ldap/ldap/v3"
	"golang.org/x/crypto/bcrypt"
)

func TestNestedGroupsOverLDAP(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	noc := uint(1)
	night := uint(2)
	dir := Build(Input{
		BaseDN: "dc=example,dc=com",
		OUs: []OU{
			{ID: noc, Name: "NOC"},
			{ID: night, ParentID: &noc, Name: "Night"},
		},
		Roles: []Role{
			{ID: 1, Name: "staff", OUID: &noc},
			{ID: 2, Name: "wdm", OUID: &noc, MemberIDs: []uint{1}},
		},
		Users: []User{{
			Username: "ada",
			Name:     "Ada Lovelace",
			Email:    "ada@example.com",
			Password: string(hash),
			OUID:     &night,
			RoleIDs:  []uint{1},
		}},
	})
	ada := findDN(dir, "uid=ada,ou=Night,ou=NOC,dc=example,dc=com")
	if ada == nil {
		t.Fatal("ada was not published under ou=Night,ou=NOC")
	}
	staff := "cn=staff,ou=NOC,dc=example,dc=com"
	wdm := "cn=wdm,ou=NOC,dc=example,dc=com"
	if !hasVal(ada, "memberof", wdm) || !hasVal(ada, "memberof", staff) {
		t.Fatalf("memberOf = %v, want staff and wdm", ada.Attrs["memberof"])
	}
	if len(ada.DirectMemberOf) != 1 || normalizeDN(ada.DirectMemberOf[0]) != normalizeDN(staff) {
		t.Fatalf("direct memberOf = %v", ada.DirectMemberOf)
	}

	addr := freeAddr(t)
	cfg := dir.Snapshot(true, addr, "", "bind-secret")
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Options{Fetch: func(context.Context) ([]byte, error) { return body, nil }})

	conn := dialLDAP(t, addr)
	defer conn.Close()
	if err := conn.Bind(cfg.BindDN, "bind-secret"); err != nil {
		t.Fatal(err)
	}
	// Active Directory in-chain: wdm's member includes ada through staff.
	chain := "(member:" + MatchingRuleInChain + ":=" + ldap.EscapeFilter(ada.DN) + ")"
	res, err := conn.Search(ldap.NewSearchRequest(
		cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		chain, []string{"cn"}, nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range res.Entries {
		got[normalizeDN(e.DN)] = true
	}
	if !got[normalizeDN(staff)] || !got[normalizeDN(wdm)] || len(got) != 2 {
		t.Fatalf("in-chain hits = %v", dns(res.Entries))
	}

	ok, info, err := ldapauth.Authenticate(ldapauth.Config{
		Host:         "127.0.0.1",
		Port:         Port(addr),
		TLSMode:      "none",
		BindDN:       cfg.BindDN,
		BindPassword: "bind-secret",
		BaseDN:       cfg.BaseDN,
		UserFilter:   "(uid=%s)",
		AttrEmail:    "mail",
		AttrGroups:   "memberOf",
	}, "ada", "secret")
	if err != nil || !ok || info == nil {
		t.Fatalf("authenticate ok=%v info=%v err=%v", ok, info, err)
	}
	if !hasFold(info.Groups, wdm) || !hasFold(info.Groups, staff) {
		t.Fatalf("groups = %v", info.Groups)
	}
	if info.Email != "ada@example.com" {
		t.Fatalf("email = %q", info.Email)
	}
	bad, _, err := ldapauth.Authenticate(ldapauth.Config{
		Host: "127.0.0.1", Port: Port(addr), TLSMode: "none",
		BindDN: cfg.BindDN, BindPassword: "bind-secret",
		BaseDN: cfg.BaseDN, UserFilter: "(uid=%s)", AttrGroups: "memberOf",
	}, "ada", "nope")
	if err != nil || bad {
		t.Fatalf("bad password ok=%v err=%v", bad, err)
	}
}

func findDN(d *Directory, dn string) *Entry {
	return d.byDN[normalizeDN(dn)]
}

func hasVal(e *Entry, attr, want string) bool {
	for _, v := range e.Attrs[attr] {
		if normalizeDN(v) == normalizeDN(want) {
			return true
		}
	}
	return false
}

func hasFold(vals []string, want string) bool {
	for _, v := range vals {
		if normalizeDN(v) == normalizeDN(want) {
			return true
		}
	}
	return false
}

func stringsEqualFold(a, b string) bool {
	return normalizeDN(a) == normalizeDN(b)
}

func TestLDAPSUsesStoreCertificate(t *testing.T) {
	dir := t.TempDir()
	writeTestPair(t, filepath.Join(dir, "hub.crt"), filepath.Join(dir, "hub.key"), "hub")
	store := filepath.Join(dir, "lego", "certificates")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestPair(t, filepath.Join(store, "_.ldap.example.crt"), filepath.Join(store, "_.ldap.example.key"), "ldaps")

	plain := freeAddr(t)
	secure := freeAddr(t)
	cfg := Build(Input{BaseDN: "dc=example,dc=com"}).Snapshot(true, plain, secure, "")
	cfg.TLSCertificate = "ldap"
	cfg.TLSDomain = "*.ldap.example"
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Options{
		TLSCert:     filepath.Join(dir, "hub.crt"),
		TLSKey:      filepath.Join(dir, "hub.key"),
		LegoStorage: filepath.Join(dir, "lego"),
		Fetch:       func(context.Context) ([]byte, error) { return body, nil },
	})

	var last error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := tls.Dial("tcp", secure, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err == nil {
			cn := conn.ConnectionState().PeerCertificates[0].Subject.CommonName
			conn.Close()
			if cn != "ldaps" {
				t.Fatalf("certificate CN = %s", cn)
			}
			return
		}
		last = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(last)
}

func writeTestPair(t *testing.T, certPath, keyPath, cn string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{cn + ".example"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

func dns(entries []*ldap.Entry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.DN)
	}
	return out
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func dialLDAP(t *testing.T, addr string) *ldap.Conn {
	t.Helper()
	var last error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := ldap.DialURL("ldap://" + addr)
		if err == nil {
			return conn
		}
		last = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(last)
	return nil
}
