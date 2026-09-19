package storage

import "testing"

func TestPullCommands(t *testing.T) {
	t.Parallel()
	cases := []struct {
		platform, proto, src, dest, want string
	}{
		{"eos", "http", "http://10.0.0.5:8088/files/eos/a.swi", "flash:a.swi", "copy http://10.0.0.5:8088/files/eos/a.swi flash:a.swi"},
		{"ios-xr", "tftp", "tftp://10.0.0.5/xr/x.iso", "harddisk:x.iso", "copy tftp://10.0.0.5/xr/x.iso harddisk:x.iso"},
		{"sros", "tftp", "tftp://10.0.0.5/sros/img", "cf3:/img", "//file copy tftp://10.0.0.5/sros/img cf3:/img"},
		{"vrp", "tftp", "tftp://10.0.0.5/vrp.bin", "vrp.bin", "tftp 10.0.0.5 get vrp.bin vrp.bin"},
		{"vrp", "http", "http://10.0.0.5/files/vrp.bin", "flash:/vrp.bin", "copy http://10.0.0.5/files/vrp.bin flash:/vrp.bin"},
		{"ciscosmb", "http", "http://10.0.0.5/files/a.bin", "flash://a.bin", "copy http://10.0.0.5/files/a.bin flash://a.bin"},
	}
	for _, tc := range cases {
		cmds, err := PullCommands(tc.platform, tc.proto, tc.src, tc.dest)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.platform, tc.proto, err)
		}
		if len(cmds) != 1 || cmds[0] != tc.want {
			t.Fatalf("%s %s: got %q want %q", tc.platform, tc.proto, cmds, tc.want)
		}
	}
	if _, err := PullCommands("eos", "ftp", "x", "y"); err == nil {
		t.Fatal("ftp should fail")
	}
	if _, err := PullCommands("openroadm", "http", "x", "y"); err == nil {
		t.Fatal("openroadm should fail")
	}
}

func TestSourceURL(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		HTTPURL:  "http://10.1.2.3:8088/",
		TFTPHost: "10.1.2.3",
		SFTPHost: "10.1.2.3:2222",
		SFTPUser: "factum",
	}
	u, err := SourceURL(cfg, "http", "/eos/a.swi")
	if err != nil || u != "http://10.1.2.3:8088/files/eos/a.swi" {
		t.Fatalf("http: %q %v", u, err)
	}
	u, err = SourceURL(cfg, "tftp", "eos/a.swi")
	if err != nil || u != "tftp://10.1.2.3/eos/a.swi" {
		t.Fatalf("tftp: %q %v", u, err)
	}
	u, err = SourceURL(cfg, "sftp", "/eos/a.swi")
	if err != nil || u != "sftp://factum@10.1.2.3:2222/eos/a.swi" {
		t.Fatalf("sftp: %q %v", u, err)
	}
}

func TestNormalizeProtocol(t *testing.T) {
	t.Parallel()
	p, err := NormalizeProtocol("HTTP")
	if err != nil || p != "http" {
		t.Fatalf("got %q %v", p, err)
	}
	if _, err := NormalizeProtocol("ftp"); err == nil {
		t.Fatal("expected error")
	}
}
