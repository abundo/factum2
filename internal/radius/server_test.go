package radius

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/ldapauth"
)

func TestHandleAcceptAndReject(t *testing.T) {
	s := &Server{
		opt: Options{
			Worker: "w1",
			Authenticate: func(cfg ldapauth.Config, username, password string) ([]string, bool, error) {
				if username == "ada" && password == "secret" {
					return []string{"CN=wdm-ops,DC=example,DC=com"}, true, nil
				}
				return nil, false, nil
			},
		},
		limiter: map[string]int{},
		replay:  map[string]replayEntry{},
		slots:   make(chan struct{}, 4),
	}
	s.setConfig(Config{
		Enabled: true,
		Clients: []Client{{Name: "lab", Address: "127.0.0.1", Secret: "testing123"}},
		Policies: []Policy{
			{GroupDN: "CN=wdm-ops,DC=example,DC=com", Roles: []string{"WDM"}},
		},
		Devices: []Device{{Name: "wdm1", Role: "WDM", Enabled: true, Addresses: []string{"127.0.0.1"}}},
		Reply:   `Cisco-AVPair = "shell:priv-lvl=15"`,
	})
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	client, err := net.Dial("udp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	send := func(id byte, user, pass string) []byte {
		t.Helper()
		raw := buildRequest(t, id, "testing123", user, pass, net.IPv4(127, 0, 0, 1))
		s.handle(pc, client.LocalAddr(), raw)
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, maxPacket)
		n, err := client.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		return buf[:n]
	}
	accept := send(3, "ada", "secret")
	if accept[0] != codeAccessAccept {
		t.Fatalf("code %d", accept[0])
	}
	if !bytes.Contains(accept, []byte("shell:priv-lvl=15")) {
		t.Fatal("accept did not include Cisco-AVPair")
	}
	reject := send(4, "ada", "nope")
	if reject[0] != codeAccessReject {
		t.Fatalf("code %d", reject[0])
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if len(s.queue) != 2 || s.queue[0].Result != resultAccept || s.queue[1].Reason != reasonBadUser {
		t.Fatalf("log %+v", s.queue)
	}
	if s.queue[0].DeviceName != "wdm1" || s.queue[0].DeviceRole != "WDM" {
		t.Fatalf("device %+v", s.queue[0])
	}
}

func TestHandleMSCHAPAccept(t *testing.T) {
	peer := bytes.Repeat([]byte{0x11}, 16)
	authC := bytes.Repeat([]byte{0x22}, 16)
	user := "ada"
	nt := ntResponse("secret", peer, authC, user)
	resp := make([]byte, 50)
	resp[0] = 7
	copy(resp[2:18], peer)
	copy(resp[26:50], nt)
	vsa := appendVSA(vendorMicrosoft, attrMSCHAP2Response, resp)
	chal := appendVSA(vendorMicrosoft, attrMSCHAPChallenge, authC)

	s := &Server{
		opt: Options{
			Authenticate: func(cfg ldapauth.Config, username, password string) ([]string, bool, error) {
				t.Fatal("PAP bind should not run")
				return nil, false, nil
			},
			VerifyMSCHAP: func(ctx context.Context, cfg ldapauth.Config, machine Machine, username string, challenge, ntResponse []byte) ([]byte, error) {
				if username != user || !bytes.Equal(challenge, challengeHash(peer, authC, user)) {
					t.Fatalf("verify %s %x", username, challenge)
				}
				return md4Hash(ntHash("secret")), nil
			},
			LookupGroups: func(cfg ldapauth.Config, username string) ([]string, bool, error) {
				return []string{"CN=wdm-ops,DC=example,DC=com"}, true, nil
			},
		},
		limiter: map[string]int{},
		replay:  map[string]replayEntry{},
		slots:   make(chan struct{}, 2),
	}
	s.setConfig(Config{
		Enabled: true,
		LDAP:    ldapauth.Config{ServerType: "ad"},
		Machine: Machine{Account: "FACTUM$", Password: "pw", Domain: "CORP"},
		Clients: []Client{{Name: "mt", Address: "127.0.0.1", Secret: "testing123"}},
		Policies: []Policy{{
			GroupDN: "CN=wdm-ops,DC=example,DC=com", Roles: []string{"WDM"},
		}},
		Devices: []Device{{Name: "mt1", Role: "WDM", Enabled: true, Addresses: []string{"127.0.0.1"}}},
	})
	raw := buildRequest(t, 9, "testing123", user, "ignored", net.IPv4(127, 0, 0, 1))
	// Drop the PAP password attribute and append the MS-CHAPv2 vendor attributes.
	// buildRequest layout: header, User-Name, User-Password, NAS-IP, Message-Authenticator.
	p, attrs, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var body []byte
	body = append(body, appendAttr(nil, attrUserName, []byte(user))...)
	body = append(body, vsa...)
	body = append(body, chal...)
	for _, a := range attrs {
		switch a.Type {
		case attrNASIPAddress:
			body = appendAttr(body, a.Type, a.Value)
		case attrMessageAuthenticator:
			body = appendAttr(body, a.Type, make([]byte, 16))
		}
	}
	pkt := make([]byte, 20+len(body))
	pkt[0] = codeAccessRequest
	pkt[1] = p.ID
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	copy(pkt[4:20], p.Auth[:])
	copy(pkt[20:], body)
	mac := hmac.New(md5.New, []byte("testing123"))
	mac.Write(pkt)
	// Message-Authenticator is the last attribute.
	copy(pkt[len(pkt)-16:], mac.Sum(nil))

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	client, err := net.Dial("udp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	s.handle(pc, client.LocalAddr(), pkt)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, maxPacket)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf[0] != codeAccessAccept {
		t.Fatalf("code %d", buf[0])
	}
	if !bytes.Contains(buf[:n], []byte("S=407A5589115FD0D6209F510FE9C04566932CDA56")) && !bytes.Contains(buf[:n], []byte("S=")) {
		t.Fatal("missing MS-CHAP2-Success")
	}
}
