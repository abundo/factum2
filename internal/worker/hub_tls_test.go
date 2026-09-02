package worker

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestHubWebSocketURLIsWSS(t *testing.T) {
	got := hubWebSocketURL("dns1.example.com:8443")
	if got != "wss://dns1.example.com:8443/hub" {
		t.Fatalf("hubWebSocketURL = %q, want wss://…/hub", got)
	}
	if strings.HasPrefix(got, "ws://") {
		t.Fatalf("plaintext ws:// URL: %q", got)
	}
}

func TestHubTLSClientConfigSkipVerify(t *testing.T) {
	cfg, err := hubTLSClientConfig(models.WorkerNode{TLSSkipVerify: true, TLSCA: "not-pem"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("want InsecureSkipVerify")
	}
	if cfg.RootCAs != nil {
		t.Fatal("skip-verify must ignore tls_ca")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion %d, want TLS 1.2", cfg.MinVersion)
	}
	if len(cfg.NextProtos) != 1 || cfg.NextProtos[0] != "http/1.1" {
		t.Fatalf("NextProtos %v, want [http/1.1]", cfg.NextProtos)
	}
}

func TestHubTLSClientConfigInvalidPEM(t *testing.T) {
	_, err := hubTLSClientConfig(models.WorkerNode{Name: "n1", TLSCA: "not-a-cert"})
	if err == nil {
		t.Fatal("want error for invalid PEM")
	}
	if !strings.Contains(err.Error(), "tls_ca") {
		t.Fatalf("err = %v, want tls_ca", err)
	}
}

func TestHubTLSClientConfigUsesPEMPool(t *testing.T) {
	certPEM, _ := generateHubTestCert(t, "127.0.0.1")
	cfg, err := hubTLSClientConfig(models.WorkerNode{TLSCA: string(certPEM)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("want RootCAs from tls_ca")
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("must verify when tls_ca is set")
	}
}

func TestConnectOnceWSSWithCA(t *testing.T) {
	srv, _ := startHubTLSTestServer(t, "secret")
	defer srv.Close()

	node := models.WorkerNode{
		Name:    "n1",
		Address: hubTestAddr(t, srv),
		Token:   "secret",
		TLSCA:   pemFromTestCert(srv),
	}
	assertConnectOnceHello(t, node)
}

func TestConnectOnceWSSSkipVerify(t *testing.T) {
	srv, _ := startHubTLSTestServer(t, "secret")
	defer srv.Close()

	node := models.WorkerNode{
		Name:          "n1",
		Address:       hubTestAddr(t, srv),
		Token:         "secret",
		TLSSkipVerify: true,
	}
	assertConnectOnceHello(t, node)
}

func TestConnectOnceWSSRejectsSANMismatch(t *testing.T) {
	certPEM, keyPEM := generateHubTestCert(t, "wrong.example")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	w := New(&util.ConfigWorker{
		Token: "secret",
		Commands: map[string]util.ConfigWorkerCommand{
			"dns": {Cmd: "/bin/true"},
		},
	})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(w.handleHubConn))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, NextProtos: []string{"http/1.1"}}
	srv.StartTLS()
	defer srv.Close()

	m := NewRemoteManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = m.connectOnce(ctx, models.WorkerNode{
		Name:    "n1",
		Address: hubTestAddr(t, srv),
		Token:   "secret",
		TLSCA:   string(certPEM),
	})
	if err == nil {
		t.Fatal("dial succeeded against cert whose SAN does not match Address")
	}
	if !strings.Contains(err.Error(), "tls") && !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("err = %v, want a TLS/certificate verification error", err)
	}
}

func TestConnectOnceWSSRejectsUntrustedCert(t *testing.T) {
	srv, _ := startHubTLSTestServer(t, "secret")
	defer srv.Close()

	m := NewRemoteManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := m.connectOnce(ctx, models.WorkerNode{
		Name:    "n1",
		Address: hubTestAddr(t, srv),
		Token:   "secret",
	})
	if err == nil {
		t.Fatal("dial succeeded against untrusted cert")
	}
}

func TestConnectOnceWSSRejectsInvalidCA(t *testing.T) {
	m := NewRemoteManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := m.connectOnce(ctx, models.WorkerNode{
		Name:  "n1",
		TLSCA: "not-pem",
	})
	if err == nil || !strings.Contains(err.Error(), "tls_ca") {
		t.Fatalf("err = %v, want tls_ca", err)
	}
}

func TestStartRefusesMissingTLS(t *testing.T) {
	w := New(&util.ConfigWorker{
		Listen:   "127.0.0.1:0",
		Commands: map[string]util.ConfigWorkerCommand{"x": {Cmd: "/bin/true"}},
	})
	err := w.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded without tls_cert/tls_key")
	}
	if !strings.Contains(err.Error(), "tls_cert") {
		t.Fatalf("err = %v, want tls_cert", err)
	}
}

func TestStartRefusesUnloadableTLS(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "hub.crt")
	keyFile := filepath.Join(dir, "hub.key")
	if err := os.WriteFile(certFile, []byte("not-a-cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("not-a-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := New(&util.ConfigWorker{
		Listen:   "127.0.0.1:0",
		TLSCert:  certFile,
		TLSKey:   keyFile,
		Commands: map[string]util.ConfigWorkerCommand{"x": {Cmd: "/bin/true"}},
	})
	err := w.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded with unloadable cert")
	}
	if !strings.Contains(err.Error(), "tls_cert") {
		t.Fatalf("err = %v, want tls_cert/tls_key", err)
	}
}

func startHubTLSTestServer(t *testing.T, token string) (*httptest.Server, *Worker) {
	t.Helper()
	w := New(&util.ConfigWorker{
		Token: token,
		Commands: map[string]util.ConfigWorkerCommand{
			"dns": {Cmd: "/bin/true"},
		},
	})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(w.handleHubConn))
	srv.TLS = &tls.Config{NextProtos: []string{"http/1.1"}}
	srv.StartTLS()
	return srv, w
}

func hubTestAddr(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func pemFromTestCert(srv *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
}

func assertConnectOnceHello(t *testing.T, node models.WorkerNode) {
	t.Helper()
	m := NewRemoteManager(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := m.connectOnce(ctx, node)
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := m.StatusAll()[node.Name]
		if st.Connected {
			if len(st.Roles) != 1 || st.Roles[0] != "dns" {
				t.Fatalf("roles %v, want [dns]", st.Roles)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("connectOnce did not return after cancel")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("node %s did not connect, last=%+v", node.Name, m.StatusAll()[node.Name])
}

func writeTestHubTLS(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	certPEM, keyPEM := generateHubTestCert(t, "127.0.0.1")
	dir := t.TempDir()
	certFile = filepath.Join(dir, "hub.crt")
	keyFile = filepath.Join(dir, "hub.key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func generateHubTestCert(t *testing.T, hosts ...string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "factum2-hub-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}
