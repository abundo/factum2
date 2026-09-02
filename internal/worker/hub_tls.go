package worker

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/abundo/factum2/models"
	"github.com/gorilla/websocket"
)

// hubTLSMinVersion is TLS 1.2. The hub carries config secrets (NetBox
// tokens, LibreNMS keys, device-sync passwords, SMTP passwords) in
// EnvelopeResponse; there is no ws:// fallback and no application-level
// payload cipher.
const hubTLSMinVersion = uint16(tls.VersionTLS12)

// hubTLSNextProtos is HTTP/1.1 only. net/http's default TLS NextProtos
// include "h2", and WebSocket upgrade cannot run over HTTP/2.
var hubTLSNextProtos = []string{"http/1.1"}

func hubWebSocketURL(address string) string {
	u := url.URL{Scheme: "wss", Host: address, Path: HubPath}
	return u.String()
}

func hubTLSClientConfig(node models.WorkerNode) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion:         hubTLSMinVersion,
		NextProtos:         hubTLSNextProtos,
		InsecureSkipVerify: node.TLSSkipVerify,
	}
	if node.TLSSkipVerify || node.TLSCA == "" {
		return cfg, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(node.TLSCA)) {
		return nil, fmt.Errorf("worker node %q: tls_ca is not valid PEM", node.Name)
	}
	cfg.RootCAs = pool
	return cfg, nil
}

func hubDialer(node models.WorkerNode) (*websocket.Dialer, error) {
	tlsCfg, err := hubTLSClientConfig(node)
	if err != nil {
		return nil, err
	}
	return &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
		TLSClientConfig:  tlsCfg,
	}, nil
}

func hubServerTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: hubTLSMinVersion,
		NextProtos: hubTLSNextProtos,
	}
}
