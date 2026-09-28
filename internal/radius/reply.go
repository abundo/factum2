package radius

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// DefaultReply is sent on Access-Accept until an administrator saves a
// different set. Devices ignore vendor attributes that are not theirs.
// Each line is one attribute. A leading # skips the line. A # after the
// value, outside quotes, is a comment and is not sent. =, := and += all
// add one attribute, so a repeated name is a second copy (FreeRADIUS +=).
// An unknown name can be written as 26.<vendor>.<type> = value.
const DefaultReply = `# Smartoptics (vendor 30826, attribute 1, string)
Smartoptics-Userrole1 = "admin"
# Service-Type = Administrative-User
Service-Type = NAS-Prompt-User
# Cisco login enable
Cisco-AVPair = "shell:priv-lvl=15"
# Arista login enable
Arista-AVPair = "shell:priv-lvl=15"
Arista-AVPair = "shell:roles=network-admin"
# Nokia SR OS. "both" is console and ftp. 4 adds netconf.
Timetra-Access = both
Timetra-Access = 4
# Timetra-Access = console
# Timetra-Access = ftp
# Timetra-Access = netconf
# Timetra-Access = grpc
Timetra-Profile = "administrative"
Timetra-Default-Action = permit-all
MikroTik-Group = "write"
# Huawei login enable. Privilege is an integer, 0-15.
Huawei-Exec-Privilege = 3
`

type attrKind int

const (
	kindString attrKind = iota
	kindInt
)

type attrDef struct {
	vendor uint32 // 0 is a standard attribute
	typ    byte
	kind   attrKind
	values map[string]uint32
}

func enum(pairs ...string) map[string]uint32 {
	out := make(map[string]uint32, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		n, _ := strconv.ParseUint(pairs[i+1], 10, 32)
		out[pairs[i]] = uint32(n)
	}
	return out
}

// Names are matched case-insensitively.
var replyDictionary = map[string]attrDef{
	"service-type": {
		typ: 6, kind: kindInt,
		values: enum(
			"login-user", "1",
			"framed-user", "2",
			"administrative-user", "6",
			"nas-prompt-user", "7",
		),
	},
	"cisco-avpair":          {vendor: 9, typ: 1, kind: kindString},
	"arista-avpair":         {vendor: 30065, typ: 1, kind: kindString},
	"smartoptics-userrole1": {vendor: 30826, typ: 1, kind: kindString},
	"timetra-access": {
		vendor: 6527, typ: 1, kind: kindInt,
		values: enum("ftp", "1", "console", "2", "both", "3", "netconf", "4", "grpc", "8"),
	},
	"timetra-profile": {vendor: 6527, typ: 4, kind: kindString},
	"timetra-default-action": {
		vendor: 6527, typ: 5, kind: kindInt,
		values: enum("permit-all", "1", "deny-all", "2", "none", "3"),
	},
	"huawei-exec-privilege": {vendor: 2011, typ: 29, kind: kindInt},
	"mikrotik-group":        {vendor: 14988, typ: 3, kind: kindString},
}

// ReplyText returns the reply lines to send. A nil stored value means the
// built-in set. An empty string sends no vendor attributes.
func ReplyText(stored *string) string {
	if stored == nil {
		return DefaultReply
	}
	return *stored
}

// ValidateReply reports whether text can be encoded into an Access-Accept.
func ValidateReply(text string) error {
	_, err := EncodeReply(text)
	return err
}

// EncodeReply turns reply lines into RADIUS attribute bytes. Message-Authenticator
// is not included; the caller puts that first.
func EncodeReply(text string) ([]byte, error) {
	var out []byte
	n := 0
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		attr, err := encodeReplyLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, attr...)
		n++
		if n > 40 {
			return nil, fmt.Errorf("more than 40 reply attributes")
		}
	}
	if len(out) > 3500 {
		return nil, fmt.Errorf("reply attributes are too large")
	}
	return out, nil
}

func encodeReplyLine(line string) ([]byte, error) {
	line = stripInlineComment(line)
	name, value, err := splitReply(line)
	if err != nil {
		return nil, err
	}
	def, ok := lookupReply(name)
	if !ok {
		return nil, fmt.Errorf("unknown attribute %q", name)
	}
	val, err := encodeValue(def, value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if def.vendor == 0 {
		return appendAttr(nil, def.typ, val), nil
	}
	return appendVSA(def.vendor, def.typ, val), nil
}

// stripInlineComment drops a # comment that is outside quotes. A # inside
// a quoted value is part of the value.
func stripInlineComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote && (i == 0 || unicode.IsSpace(rune(line[i-1]))) {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return line
}

func splitReply(line string) (name, value string, err error) {
	for _, op := range []string{":=", "+=", "="} {
		if i := strings.Index(line, op); i > 0 {
			name = strings.TrimSpace(line[:i])
			value = strings.TrimSpace(line[i+len(op):])
			if name == "" || value == "" {
				return "", "", fmt.Errorf("expected name = value")
			}
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				value = value[1 : len(value)-1]
			}
			if value == "" || strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return "", "", fmt.Errorf("empty or invalid value")
			}
			return name, value, nil
		}
	}
	return "", "", fmt.Errorf("expected name = value")
}

func lookupReply(name string) (attrDef, bool) {
	if def, ok := replyDictionary[strings.ToLower(name)]; ok {
		return def, true
	}
	const pfx = "26."
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, pfx) {
		return attrDef{}, false
	}
	rest := lower[len(pfx):]
	vendorStr, typeStr, ok := strings.Cut(rest, ".")
	if !ok {
		return attrDef{}, false
	}
	kindStr := ""
	if i := strings.IndexByte(typeStr, ':'); i >= 0 {
		kindStr = typeStr[i+1:]
		typeStr = typeStr[:i]
	}
	vendor, errV := strconv.ParseUint(vendorStr, 10, 32)
	typ, errT := strconv.ParseUint(typeStr, 10, 8)
	if errV != nil || errT != nil || typ == 0 || vendor == 0 {
		return attrDef{}, false
	}
	def := attrDef{vendor: uint32(vendor), typ: byte(typ), kind: kindString}
	switch kindStr {
	case "", "string":
	case "integer":
		def.kind = kindInt
	default:
		return attrDef{}, false
	}
	return def, true
}

func encodeValue(def attrDef, value string) ([]byte, error) {
	switch def.kind {
	case kindString:
		if len(value) > 247 {
			return nil, fmt.Errorf("value is too long")
		}
		return []byte(value), nil
	case kindInt:
		n, err := replyInt(def, value)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, n)
		return buf, nil
	default:
		return nil, fmt.Errorf("unsupported type")
	}
}

func replyInt(def attrDef, value string) (uint32, error) {
	if n, err := strconv.ParseUint(value, 10, 32); err == nil {
		return uint32(n), nil
	}
	if def.values != nil {
		if n, ok := def.values[strings.ToLower(value)]; ok {
			return n, nil
		}
	}
	return 0, fmt.Errorf("unknown value %q", value)
}

func appendVSA(vendor uint32, typ byte, val []byte) []byte {
	body := make([]byte, 6+len(val))
	binary.BigEndian.PutUint32(body[:4], vendor)
	body[4] = typ
	body[5] = byte(2 + len(val))
	copy(body[6:], val)
	return appendAttr(nil, 26, body)
}
