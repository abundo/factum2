package radius

import (
	"crypto/des"
	"crypto/sha1"
	"encoding/binary"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/md4"
)

// md4Hash is MD4(b).
func md4Hash(b []byte) []byte {
	h := md4.New()
	h.Write(b)
	return h.Sum(nil)
}

// Microsoft vendor ID and the MS-CHAPv2 attributes from RFC 2548.
const (
	vendorMicrosoft     = 311
	attrMSCHAPChallenge = 11
	attrMSCHAP2Response = 25
	attrMSCHAP2Success  = 26
)

// mschap2 is one MS-CHAPv2 response taken from an Access-Request.
type mschap2 struct {
	ident byte
	peer  []byte // 16
	auth  []byte // 16, the authenticator challenge
	nt    []byte // 24
	user  string // name used in ChallengeHash, without a domain
}

var (
	magic1 = []byte("Magic server to client signing constant")
	magic2 = []byte("Pad to make it do more than one iteration")
)

// parseMSCHAP2 returns the MS-CHAPv2 material when the request carries it.
func parseMSCHAP2(attrs []attribute, reqAuth []byte) (mschap2, bool) {
	var challenge, response []byte
	for _, a := range attrs {
		if a.Type != 26 || len(a.Value) < 6 {
			continue
		}
		if binary.BigEndian.Uint32(a.Value[:4]) != vendorMicrosoft {
			continue
		}
		if int(a.Value[5]) != len(a.Value)-4 {
			continue
		}
		val := a.Value[6:]
		switch a.Value[4] {
		case attrMSCHAPChallenge:
			challenge = append([]byte(nil), val...)
		case attrMSCHAP2Response:
			response = append([]byte(nil), val...)
		}
	}
	if len(response) != 50 {
		return mschap2{}, false
	}
	if len(challenge) != 16 {
		if len(reqAuth) != 16 {
			return mschap2{}, false
		}
		challenge = append([]byte(nil), reqAuth...)
	}
	return mschap2{
		ident: response[0],
		peer:  append([]byte(nil), response[2:18]...),
		auth:  challenge,
		nt:    append([]byte(nil), response[26:50]...),
	}, true
}

// challengeHash is the 8-octet challenge a domain controller verifies
// (RFC 2759). username is the bare login name, without a domain.
func challengeHash(peer, authenticator []byte, username string) []byte {
	h := sha1.New()
	h.Write(peer)
	h.Write(authenticator)
	h.Write([]byte(username))
	return h.Sum(nil)[:8]
}

// authenticatorResponse is the 20-octet value inside MS-CHAP2-Success.
// passwordHashHash is MD4(NT hash), which a domain controller returns as
// the NTLMv1 user session key after it accepts the response.
func authenticatorResponse(passwordHashHash, ntResponse, peer, authenticator []byte, username string) []byte {
	d := sha1.New()
	d.Write(passwordHashHash)
	d.Write(ntResponse)
	d.Write(magic1)
	digest := d.Sum(nil)
	h := sha1.New()
	h.Write(digest)
	h.Write(challengeHash(peer, authenticator, username))
	h.Write(magic2)
	return h.Sum(nil)
}

// mschap2Success is the Microsoft vendor attribute MikroTik expects in
// Access-Accept. Without it the router discards the accept.
func mschap2Success(m mschap2, passwordHashHash []byte) []byte {
	auth := authenticatorResponse(passwordHashHash, m.nt, m.peer, m.auth, m.user)
	val := make([]byte, 1+2+40)
	val[0] = m.ident
	copy(val[1:], "S=")
	const hexdigits = "0123456789ABCDEF"
	for i, b := range auth {
		val[3+i*2] = hexdigits[b>>4]
		val[4+i*2] = hexdigits[b&0x0f]
	}
	return appendVSA(vendorMicrosoft, attrMSCHAP2Success, val)
}

// samAccount strips DOMAIN\user and user@domain down to the account name
// Active Directory and ChallengeHash both use.
func samAccount(username string) string {
	username = strings.TrimSpace(username)
	if i := strings.LastIndex(username, `\`); i >= 0 {
		username = username[i+1:]
	}
	if i := strings.IndexByte(username, '@'); i >= 0 {
		username = username[:i]
	}
	return username
}

// ntHash is MD4 of the UTF-16LE password. Used by tests and by the
// authenticator-response check. A domain controller does not reveal it.
func ntHash(password string) []byte {
	u := utf16.Encode([]rune(password))
	b := make([]byte, len(u)*2)
	for i, r := range u {
		b[i*2] = byte(r)
		b[i*2+1] = byte(r >> 8)
	}
	return md4Hash(b)
}

// ntResponse is the 24-octet MS-CHAPv2 NT-Response for a known password.
// The live path does not call this; the domain controller checks the
// response the router already sent. Tests use it to build a packet.
func ntResponse(password string, peer, authenticator []byte, username string) []byte {
	hash := ntHash(password)
	chal := challengeHash(peer, authenticator, username)
	return ntChallengeResponse(hash, chal)
}

func ntChallengeResponse(hash, challenge []byte) []byte {
	// Expand 16-byte NT hash to three 7-byte DES keys (RFC 2759).
	z := make([]byte, 21)
	copy(z, hash)
	out := make([]byte, 24)
	for i := 0; i < 3; i++ {
		blk, err := des.NewCipher(expandDESKey(z[i*7 : i*7+7]))
		if err != nil {
			return nil
		}
		blk.Encrypt(out[i*8:i*8+8], challenge)
	}
	return out
}

func expandDESKey(b7 []byte) []byte {
	key := make([]byte, 8)
	key[0] = b7[0] >> 1
	key[1] = ((b7[0] & 0x01) << 6) | (b7[1] >> 2)
	key[2] = ((b7[1] & 0x03) << 5) | (b7[2] >> 3)
	key[3] = ((b7[2] & 0x07) << 4) | (b7[3] >> 4)
	key[4] = ((b7[3] & 0x0f) << 3) | (b7[4] >> 5)
	key[5] = ((b7[4] & 0x1f) << 2) | (b7[5] >> 6)
	key[6] = ((b7[5] & 0x3f) << 1) | (b7[6] >> 7)
	key[7] = b7[6] & 0x7f
	for i := range key {
		key[i] <<= 1
		// Odd parity. DES requires it; Go's des package checks parity.
		var bits int
		for v := key[i]; v != 0; v >>= 1 {
			bits += int(v & 1)
		}
		if bits%2 == 0 {
			key[i] |= 1
		}
	}
	return key
}
