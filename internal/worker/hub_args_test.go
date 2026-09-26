package worker

import "testing"

func TestFilterWireArgs(t *testing.T) {
	t.Parallel()
	allow := []string{"copy", "-n=*", "--path=*", "--protocol=*", "--job", "--destination=*"}
	got, err := filterWireArgs(allow, []string{"copy", "-n", "r1", "--path", "/eos/a.swi", "--protocol", "http", "--job"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("got %#v", got)
	}
	if _, err := filterWireArgs(nil, []string{"--job"}); err == nil {
		t.Fatal("empty allow list must reject wire args")
	}
	if _, err := filterWireArgs(allow, []string{"-f", "/tmp/evil.yaml"}); err == nil {
		t.Fatal("unlisted flag must be rejected")
	}
	if _, err := filterWireArgs(allow, []string{"--destination", "flash:x\nreload"}); err == nil {
		t.Fatal("control character in a value must be rejected")
	}
	if _, err := filterWireArgs(allow, nil); err != nil {
		t.Fatal(err)
	}
}
