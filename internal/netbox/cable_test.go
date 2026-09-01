package netbox

import (
	"testing"

	"github.com/abundo/netboxtool"
)

type stubCableWriter struct {
	a, b  uint
	extra map[string]any
}

func (s *stubCableWriter) CreateCableWithOptions(a, b uint, extra map[string]any) (*netboxtool.NBCable, error) {
	s.a, s.b, s.extra = a, b, extra
	label, _ := extra["label"].(string)
	return &netboxtool.NBCable{NetboxID: 1, AInterface: a, BInterface: b, Label: label}, nil
}

func TestIsLLDPCable(t *testing.T) {
	if IsLLDPCable(nil) {
		t.Fatal("nil cable must not be LLDP-owned")
	}
	if IsLLDPCable(&netboxtool.NBCable{Label: ""}) {
		t.Fatal("unlabeled cable must not be LLDP-owned")
	}
	if IsLLDPCable(&netboxtool.NBCable{Label: "manual"}) {
		t.Fatal("manual cable must not be LLDP-owned")
	}
	if !IsLLDPCable(&netboxtool.NBCable{Label: CableLabelLLDP}) {
		t.Fatalf("label %q must be LLDP-owned", CableLabelLLDP)
	}
}

func TestCreateLLDPCable(t *testing.T) {
	stub := &stubCableWriter{}
	cable, err := CreateLLDPCable(stub, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if stub.a != 10 || stub.b != 20 {
		t.Errorf("terminations = %d, %d; want 10, 20", stub.a, stub.b)
	}
	if stub.extra["label"] != CableLabelLLDP {
		t.Errorf("extra label = %v, want %q", stub.extra["label"], CableLabelLLDP)
	}
	if cable.Label != CableLabelLLDP {
		t.Errorf("cable.Label = %q, want %q", cable.Label, CableLabelLLDP)
	}
}
