package ldapsrv

import (
	"encoding/binary"
	"errors"
	"io"
)

const maxBER = 1 << 20

var (
	errBER        = errors.New("ldap: bad encoding")
	errIndefinite = errors.New("ldap: indefinite length")
)

// ber is one BER element. kids is set when the element is constructed.
type ber struct {
	cls  byte
	cons bool
	tag  int
	raw  []byte
	kids []ber
}

func readBER(r io.Reader) (ber, error) {
	var tb [1]byte
	if _, err := io.ReadFull(r, tb[:]); err != nil {
		return ber{}, err
	}
	tag := int(tb[0] & 0x1f)
	if tag == 0x1f {
		return ber{}, errBER
	}
	n, err := readLen(r)
	if err != nil {
		return ber{}, err
	}
	if n < 0 || n > maxBER {
		return ber{}, errBER
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return ber{}, err
	}
	el := ber{cls: tb[0] >> 6, cons: tb[0]&0x20 != 0, tag: tag, raw: buf}
	if el.cons {
		el.kids, err = parseKids(buf)
		if err != nil {
			return ber{}, err
		}
	}
	return el, nil
}

func parseKids(b []byte) ([]ber, error) {
	var out []ber
	for len(b) > 0 {
		el, rest, err := parseOne(b)
		if err != nil {
			return nil, err
		}
		out = append(out, el)
		b = rest
	}
	return out, nil
}

func parseOne(b []byte) (ber, []byte, error) {
	if len(b) < 2 {
		return ber{}, nil, errBER
	}
	tag := int(b[0] & 0x1f)
	if tag == 0x1f {
		return ber{}, nil, errBER
	}
	n, rest, err := takeLen(b[1:])
	if err != nil {
		return ber{}, nil, err
	}
	if n > len(rest) || n > maxBER {
		return ber{}, nil, errBER
	}
	el := ber{cls: b[0] >> 6, cons: b[0]&0x20 != 0, tag: tag, raw: rest[:n]}
	if el.cons {
		el.kids, err = parseKids(el.raw)
		if err != nil {
			return ber{}, nil, err
		}
	}
	return el, rest[n:], nil
}

func readLen(r io.Reader) (int, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	if b[0] < 0x80 {
		return int(b[0]), nil
	}
	if b[0] == 0x80 {
		return 0, errIndefinite
	}
	n := int(b[0] & 0x7f)
	if n == 0 || n > 3 {
		return 0, errBER
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	l := 0
	for _, c := range buf {
		l = l<<8 | int(c)
	}
	return l, nil
}

func takeLen(b []byte) (int, []byte, error) {
	if len(b) == 0 {
		return 0, nil, errBER
	}
	if b[0] < 0x80 {
		return int(b[0]), b[1:], nil
	}
	if b[0] == 0x80 {
		return 0, nil, errIndefinite
	}
	n := int(b[0] & 0x7f)
	if n == 0 || n > 3 || len(b) < 1+n {
		return 0, nil, errBER
	}
	l := 0
	for i := 0; i < n; i++ {
		l = l<<8 | int(b[1+i])
	}
	return l, b[1+n:], nil
}

func (el ber) integer() (int64, error) {
	if el.cons || (el.tag != 2 && el.tag != 10) || len(el.raw) == 0 || len(el.raw) > 8 {
		return 0, errBER
	}
	var v int64
	for _, c := range el.raw {
		v = v<<8 | int64(c)
	}
	bits := len(el.raw) * 8
	if bits < 64 && el.raw[0]&0x80 != 0 {
		v |= -1 << bits
	}
	return v, nil
}

func (el ber) text() string { return string(el.raw) }

func (el ber) boolean() bool { return len(el.raw) > 0 && el.raw[0] != 0 }

func appendLen(dst []byte, n int) []byte {
	if n < 0x80 {
		return append(dst, byte(n))
	}
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(n))
	i := 0
	for i < 3 && buf[i] == 0 {
		i++
	}
	dst = append(dst, byte(0x80|(4-i)))
	return append(dst, buf[i:]...)
}

func appendTLV(dst []byte, tag byte, body []byte) []byte {
	dst = append(dst, tag)
	dst = appendLen(dst, len(body))
	return append(dst, body...)
}

func encodeInt(tag byte, v int64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v))
	i := 0
	for i < 7 {
		signPad := (buf[i] == 0 && buf[i+1]&0x80 == 0) || (buf[i] == 0xff && buf[i+1]&0x80 != 0)
		if !signPad {
			break
		}
		i++
	}
	return appendTLV(nil, tag, buf[i:])
}

func encodeString(tag byte, s string) []byte {
	return appendTLV(nil, tag, []byte(s))
}

func encodeBool(v bool) []byte {
	b := byte(0)
	if v {
		b = 0xff
	}
	return appendTLV(nil, 0x01, []byte{b})
}

func encodeMessage(id int, ops ...[]byte) []byte {
	body := encodeInt(0x02, int64(id))
	for _, op := range ops {
		body = append(body, op...)
	}
	return appendTLV(nil, 0x30, body)
}

func encodeResult(app int, code int, msg string) []byte {
	body := encodeInt(0x0a, int64(code))
	body = append(body, encodeString(0x04, "")...)
	body = append(body, encodeString(0x04, msg)...)
	return appendTLV(nil, byte(0x60|app), body)
}

func encodeEntry(dn string, attrs [][2]string) []byte {
	body := encodeString(0x04, dn)
	var list []byte
	i := 0
	for i < len(attrs) {
		name := attrs[i][0]
		var set []byte
		for i < len(attrs) && attrs[i][0] == name {
			set = append(set, encodeString(0x04, attrs[i][1])...)
			i++
		}
		one := encodeString(0x04, name)
		one = append(one, appendTLV(nil, 0x31, set)...)
		list = append(list, appendTLV(nil, 0x30, one)...)
	}
	body = append(body, appendTLV(nil, 0x30, list)...)
	return appendTLV(nil, 0x64, body)
}

func appTag(cons bool, n int) byte {
	b := byte(0x40 | n)
	if cons {
		b |= 0x20
	}
	return b
}
