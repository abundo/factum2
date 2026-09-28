package radius

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestRFC2759MSCHAP(t *testing.T) {
	authC, _ := hex.DecodeString("5B5D7C7D7B3F2F3E3C2C602132262628")
	peer, _ := hex.DecodeString("21402324255E262A28295F2B3A337C7E")
	wantNT, _ := hex.DecodeString("82309ECD8D708B5EA08FAA3981CD83544233114A3D85D6DF")
	got := ntResponse("clientPass", peer, authC, "User")
	if hex.EncodeToString(got) != hex.EncodeToString(wantNT) {
		t.Fatalf("nt response %x", got)
	}
	hash := ntHash("clientPass")
	wantHash, _ := hex.DecodeString("44EBBA8D5312B8D611474411F56989AE")
	if hex.EncodeToString(hash) != hex.EncodeToString(wantHash) {
		t.Fatalf("nt hash %x", hash)
	}
	hashHash := md4Hash(hash)
	resp := authenticatorResponse(hashHash, got, peer, authC, "User")
	if hex.EncodeToString(resp) != "407a5589115fd0d6209f510fe9c04566932cda56" {
		t.Fatalf("authenticator %x", resp)
	}
	key := expandDESKey([]byte{0xFC, 0x15, 0x6A, 0xF7, 0xED, 0xCD, 0x6C})
	if hex.EncodeToString(key) != "fd0b5b5e7f6e34d9" {
		t.Fatalf("des key %x", key)
	}
}

func TestMSCHAP2SuccessAttribute(t *testing.T) {
	authC, _ := hex.DecodeString("5B5D7C7D7B3F2F3E3C2C602132262628")
	peer, _ := hex.DecodeString("21402324255E262A28295F2B3A337C7E")
	m := mschap2{ident: 1, peer: peer, auth: authC, nt: ntResponse("clientPass", peer, authC, "User"), user: "User"}
	raw := mschap2Success(m, md4Hash(ntHash("clientPass")))
	if !bytes.Contains(raw, []byte("S=407A5589115FD0D6209F510FE9C04566932CDA56")) {
		t.Fatalf("success %q", raw)
	}
}
