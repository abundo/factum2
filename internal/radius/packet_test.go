package radius

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"
)

func TestUserPasswordVector(t *testing.T) {
	secret := []byte("testing123")
	auth := make([]byte, 16)
	for i := range auth {
		auth[i] = byte(i)
	}
	got := encryptUserPassword("secret", secret, auth)
	want, err := hex.DecodeString("e58b6ab811897a1a104607240014828b")
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("cipher %x", got)
	}
	plain, err := decryptUserPassword(got, secret, auth)
	if err != nil || plain != "secret" {
		t.Fatalf("decrypt %q %v", plain, err)
	}
	raw, err := hex.DecodeString("0107003f000102030405060708090a0b0c0d0e0f0107616c6963650212e58b6ab811897a1a104607240014828b501216473f6646bb569b20c17c3740a965cc")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyMessageAuthenticator(raw, secret); err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0x01
	if err := verifyMessageAuthenticator(raw, secret); err == nil {
		t.Fatal("tampered authenticator accepted")
	}
}

func TestResponseAuthenticatorRoundTrip(t *testing.T) {
	secret := []byte("testing123")
	reqRaw := buildRequest(t, 9, "testing123", "alice", "secret", net.IPv4(10, 0, 0, 1))
	p, _, err := parse(reqRaw)
	if err != nil {
		t.Fatal(err)
	}
	reply := response(p, codeAccessAccept, secret, "", nil)
	if reply[0] != codeAccessAccept || reply[1] != 9 {
		t.Fatalf("code/id %d/%d", reply[0], reply[1])
	}
	// Check Message-Authenticator with the request authenticator restored,
	// then the response authenticator over the signed packet.
	restored := append([]byte(nil), reply...)
	copy(restored[4:20], p.Auth[:])
	if err := verifyMessageAuthenticator(restored, secret); err != nil {
		t.Fatal(err)
	}
	h := md5.New()
	h.Write(restored)
	h.Write(secret)
	sum := h.Sum(nil)
	if hex.EncodeToString(sum) != hex.EncodeToString(reply[4:20]) {
		t.Fatal("response authenticator mismatch")
	}
	if binary.BigEndian.Uint16(reply[2:4]) != uint16(len(reply)) {
		t.Fatal("length")
	}
}

func buildRequest(t *testing.T, id byte, secret, user, pass string, nas net.IP) []byte {
	t.Helper()
	auth := make([]byte, 16)
	for i := range auth {
		auth[i] = id + byte(i)
	}
	attrs := appendAttr(nil, attrUserName, []byte(user))
	attrs = appendAttr(attrs, attrUserPassword, encryptUserPassword(pass, []byte(secret), auth))
	if v4 := nas.To4(); v4 != nil {
		attrs = appendAttr(attrs, attrNASIPAddress, []byte(v4))
	}
	attrs = appendAttr(attrs, attrMessageAuthenticator, make([]byte, 16))
	pkt := make([]byte, 20+len(attrs))
	pkt[0] = codeAccessRequest
	pkt[1] = id
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	copy(pkt[4:20], auth)
	copy(pkt[20:], attrs)
	mac := hmac.New(md5.New, []byte(secret))
	mac.Write(pkt)
	copy(pkt[len(pkt)-16:], mac.Sum(nil))
	if err := verifyMessageAuthenticator(pkt, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	return pkt
}
