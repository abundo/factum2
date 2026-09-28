// Package radius terminates RADIUS Access-Request for switch and router
// login. The worker listens on UDP, checks the packet with the NAS shared
// secret, and binds to LDAP itself. factum2 is where the policy is edited
// and where accept/reject lines are stored; it is not on the login path.
package radius

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"net"
)

const (
	codeAccessRequest byte = 1
	codeAccessAccept  byte = 2
	codeAccessReject  byte = 3

	attrUserName             byte = 1
	attrUserPassword         byte = 2
	attrNASIPAddress         byte = 4
	attrReplyMessage         byte = 18
	attrMessageAuthenticator byte = 80
	attrNASIPv6Address       byte = 95

	maxPacket = 4096
)

var (
	errShortPacket = errors.New("radius: short packet")
	errBadLength   = errors.New("radius: bad length")
	errBadAttr     = errors.New("radius: bad attribute")
	errNoMA        = errors.New("radius: missing message-authenticator")
	errBadMA       = errors.New("radius: bad message-authenticator")
	errNoPassword  = errors.New("radius: user-password missing")
)

// Packet is a parsed RADIUS message. Raw is the on-wire bytes of Length,
// which is what the Message-Authenticator covers.
type Packet struct {
	Code byte
	ID   byte
	Auth [16]byte
	Raw  []byte
}

type attribute struct {
	Type  byte
	Value []byte
}

func parse(b []byte) (Packet, []attribute, error) {
	if len(b) < 20 {
		return Packet{}, nil, errShortPacket
	}
	length := int(binary.BigEndian.Uint16(b[2:4]))
	if length < 20 || length > len(b) || length > maxPacket {
		return Packet{}, nil, errBadLength
	}
	var p Packet
	p.Code = b[0]
	p.ID = b[1]
	copy(p.Auth[:], b[4:20])
	p.Raw = append([]byte(nil), b[:length]...)
	attrs, err := attributes(p.Raw[20:])
	if err != nil {
		return Packet{}, nil, err
	}
	return p, attrs, nil
}

func attributes(b []byte) ([]attribute, error) {
	var out []attribute
	for len(b) > 0 {
		if len(b) < 2 {
			return nil, errBadAttr
		}
		n := int(b[1])
		if n < 2 || n > len(b) {
			return nil, errBadAttr
		}
		out = append(out, attribute{Type: b[0], Value: append([]byte(nil), b[2:n]...)})
		b = b[n:]
	}
	return out, nil
}

func attrValue(attrs []attribute, typ byte) []byte {
	for _, a := range attrs {
		if a.Type == typ {
			return a.Value
		}
	}
	return nil
}

func countAttr(attrs []attribute, typ byte) int {
	n := 0
	for _, a := range attrs {
		if a.Type == typ {
			n++
		}
	}
	return n
}

// verifyMessageAuthenticator checks RFC 2869 Message-Authenticator over raw.
// The attribute value is replaced with 16 zero bytes before HMAC-MD5.
func verifyMessageAuthenticator(raw, secret []byte) error {
	if len(raw) < 20 {
		return errShortPacket
	}
	length := int(binary.BigEndian.Uint16(raw[2:4]))
	if length < 20 || length > len(raw) {
		return errBadLength
	}
	body := raw[:length]
	attrs, err := attributes(body[20:])
	if err != nil {
		return err
	}
	if countAttr(attrs, attrMessageAuthenticator) != 1 {
		return errNoMA
	}
	off := 20
	var got []byte
	for off < length {
		n := int(body[off+1])
		if body[off] == attrMessageAuthenticator {
			if n != 18 {
				return errBadMA
			}
			got = body[off+2 : off+18]
			break
		}
		off += n
	}
	buf := append([]byte(nil), body...)
	for i := 0; i < 16; i++ {
		buf[off+2+i] = 0
	}
	mac := hmac.New(md5.New, secret)
	mac.Write(buf)
	if subtle.ConstantTimeCompare(mac.Sum(nil), got) != 1 {
		return errBadMA
	}
	return nil
}

// response builds an Access-Accept or Access-Reject. Message-Authenticator
// is calculated with the request authenticator in the header (RFC 2869),
// then the response authenticator replaces it (RFC 2865).
func response(req Packet, code byte, secret []byte, replyMessage string, extra []byte) []byte {
	attrs := appendAttr(nil, attrMessageAuthenticator, make([]byte, 16))
	if replyMessage != "" {
		msg := replyMessage
		if len(msg) > 200 {
			msg = msg[:200]
		}
		attrs = appendAttr(attrs, attrReplyMessage, []byte(msg))
	}
	attrs = append(attrs, extra...)
	pkt := make([]byte, 20+len(attrs))
	pkt[0] = code
	pkt[1] = req.ID
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	copy(pkt[4:20], req.Auth[:])
	copy(pkt[20:], attrs)
	mac := hmac.New(md5.New, secret)
	mac.Write(pkt)
	copy(pkt[22:38], mac.Sum(nil))
	sum := md5.New()
	sum.Write(pkt)
	sum.Write(secret)
	copy(pkt[4:20], sum.Sum(nil))
	return pkt
}

func appendAttr(dst []byte, typ byte, val []byte) []byte {
	dst = append(dst, typ, byte(len(val)+2))
	return append(dst, val...)
}

func decryptUserPassword(cipher, secret, auth []byte) (string, error) {
	if len(cipher) == 0 || len(cipher)%16 != 0 || len(cipher) > 128 {
		return "", errNoPassword
	}
	out := make([]byte, len(cipher))
	prev := auth
	for i := 0; i < len(cipher); i += 16 {
		h := md5.New()
		h.Write(secret)
		h.Write(prev)
		sum := h.Sum(nil)
		for j := 0; j < 16; j++ {
			out[i+j] = cipher[i+j] ^ sum[j]
		}
		prev = cipher[i : i+16]
	}
	n := len(out)
	for n > 0 && out[n-1] == 0 {
		n--
	}
	return string(out[:n]), nil
}

func encryptUserPassword(password string, secret, auth []byte) []byte {
	plain := []byte(password)
	if len(plain)%16 != 0 {
		pad := 16 - len(plain)%16
		plain = append(plain, make([]byte, pad)...)
	}
	out := make([]byte, len(plain))
	prev := auth
	for i := 0; i < len(plain); i += 16 {
		h := md5.New()
		h.Write(secret)
		h.Write(prev)
		sum := h.Sum(nil)
		for j := 0; j < 16; j++ {
			out[i+j] = plain[i+j] ^ sum[j]
		}
		prev = out[i : i+16]
	}
	return out
}

func nasIP(attrs []attribute) string {
	if v := attrValue(attrs, attrNASIPAddress); len(v) == 4 {
		if ip := net.IP(v).To4(); ip != nil {
			return ip.String()
		}
	}
	if v := attrValue(attrs, attrNASIPv6Address); len(v) == 16 {
		ip := net.IP(v)
		if ip.To16() != nil {
			return ip.String()
		}
	}
	return ""
}
