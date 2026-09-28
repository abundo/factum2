package radius

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestDefaultReplyEncodesVendorAttributes(t *testing.T) {
	raw, err := EncodeReply(DefaultReply)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := attributes(raw)
	if err != nil {
		t.Fatal(err)
	}
	var service, cisco, arista, smart, access, profile, action, huawei int
	for _, a := range attrs {
		switch a.Type {
		case 6:
			service++
			if binary.BigEndian.Uint32(a.Value) != 7 {
				t.Fatalf("service-type %d", binary.BigEndian.Uint32(a.Value))
			}
		case 26:
			if len(a.Value) < 6 {
				t.Fatal("short vsa")
			}
			vendor := binary.BigEndian.Uint32(a.Value[:4])
			typ := a.Value[4]
			val := a.Value[6:]
			switch {
			case vendor == 9 && typ == 1 && string(val) == "shell:priv-lvl=15":
				cisco++
			case vendor == 30065 && typ == 1:
				arista++
			case vendor == 30826 && typ == 1 && string(val) == "admin":
				smart++
			case vendor == 6527 && typ == 1:
				access++
			case vendor == 6527 && typ == 4 && string(val) == "administrative":
				profile++
			case vendor == 6527 && typ == 5 && binary.BigEndian.Uint32(val) == 1:
				action++
			case vendor == 2011 && typ == 29 && binary.BigEndian.Uint32(val) == 3:
				huawei++
			case vendor == 14988 && typ == 3 && string(val) == "write":
			default:
				t.Fatalf("unexpected vsa vendor %d type %d %q", vendor, typ, val)
			}
		default:
			t.Fatalf("unexpected attribute %d", a.Type)
		}
	}
	if service != 1 || cisco != 1 || arista != 2 || smart != 1 || access != 2 || profile != 1 || action != 1 || huawei != 1 {
		t.Fatalf("counts service=%d cisco=%d arista=%d smart=%d access=%d profile=%d action=%d huawei=%d",
			service, cisco, arista, smart, access, profile, action, huawei)
	}
	var sawBoth, sawNetconf bool
	for _, a := range attrs {
		if a.Type == 26 && binary.BigEndian.Uint32(a.Value[:4]) == 6527 && a.Value[4] == 1 {
			switch binary.BigEndian.Uint32(a.Value[6:]) {
			case 3:
				sawBoth = true
			case 4:
				sawNetconf = true
			}
		}
	}
	if !sawBoth || !sawNetconf {
		t.Fatal("timetra-access values")
	}
}

func TestReplyRoundTripKeepsMessageAuthenticator(t *testing.T) {
	extra, err := EncodeReply(`Cisco-AVPair = "shell:priv-lvl=15"`)
	if err != nil {
		t.Fatal(err)
	}
	reqRaw := buildRequest(t, 4, "testing123", "ada", "secret", nil)
	p, _, err := parse(reqRaw)
	if err != nil {
		t.Fatal(err)
	}
	reply := response(p, codeAccessAccept, []byte("testing123"), "", extra)
	if !bytes.Contains(reply, []byte("shell:priv-lvl=15")) {
		t.Fatal("av pair missing")
	}
	restored := append([]byte(nil), reply...)
	copy(restored[4:20], p.Auth[:])
	if err := verifyMessageAuthenticator(restored, []byte("testing123")); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownReplyAttribute(t *testing.T) {
	if err := ValidateReply(`Not-A-Real-Attribute = "x"`); err == nil {
		t.Fatal("accepted an unknown attribute")
	}
	raw, err := EncodeReply(`26.30826.2 = "operator"`)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("operator")) {
		t.Fatal(raw)
	}
}
