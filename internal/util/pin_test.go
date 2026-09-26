package util

import "testing"

func TestPinnedPath(t *testing.T) {
	t.Parallel()
	got, err := PinnedPath("/etc/dnsmgr2/records", "/etc/dnsmgr2/records", "dns_dest_file")
	if err != nil || got != "/etc/dnsmgr2/records" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := PinnedPath("", "/tmp/x", "dns_dest_file"); err == nil {
		t.Fatal("empty pin must fail")
	}
	if _, err := PinnedPath("/etc/dnsmgr2/records", "/tmp/evil", "dns_dest_file"); err == nil {
		t.Fatal("mismatch must fail")
	}
	if _, err := PinnedPath("records", "/etc/dnsmgr2/records", "dns_dest_file"); err == nil {
		t.Fatal("relative pin must fail")
	}
}

func TestPinnedExecutable(t *testing.T) {
	t.Parallel()
	got, err := PinnedExecutable("/usr/local/bin/lego", "/usr/local/bin/lego", "lego_bin")
	if err != nil || got != "/usr/local/bin/lego" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = PinnedExecutable("lego", "lego", "lego_bin")
	if err != nil || got != "lego" {
		t.Fatalf("bare name: %q %v", got, err)
	}
	if _, err := PinnedExecutable("lego", "/tmp/evil", "lego_bin"); err == nil {
		t.Fatal("mismatch must fail")
	}
	if _, err := PinnedExecutable("../lego", "lego", "lego_bin"); err == nil {
		t.Fatal("relative path must fail")
	}
}
