package util

import "testing"

func TestResolvePinnedKey(t *testing.T) {
	t.Parallel()
	paths := ConfigWorkerPaths{
		IcingaHostsFile: "/etc/icinga2/conf.d/factum2-hosts.conf",
		IcingaUsersFile: "/etc/icinga2/conf.d/factum2-users.conf",
		LegoBin:         "lego",
	}

	got, err := ResolvePinnedKey(paths, KeyIcingaUsersFile, KeyIcingaUsersFile, true, false)
	if err != nil || got != paths.IcingaUsersFile {
		t.Fatalf("users key: %q %v", got, err)
	}

	// A destination field may name another worker.paths key. The path
	// still comes only from this host.
	got, err = ResolvePinnedKey(paths, KeyIcingaHostsFile, KeyIcingaUsersFile, true, false)
	if err != nil || got != paths.IcingaHostsFile {
		t.Fatalf("hosts key for users field: %q %v", got, err)
	}

	got, err = ResolvePinnedKey(paths, "", KeyIcingaUsersFile, true, false)
	if err != nil || got != paths.IcingaUsersFile {
		t.Fatalf("empty uses default key: %q %v", got, err)
	}

	got, err = ResolvePinnedKey(paths, "", KeyIcingaCertsFile, true, false)
	if err != nil || got != "" {
		t.Fatalf("optional empty pin: %q %v", got, err)
	}

	if _, err := ResolvePinnedKey(paths, "", KeyDNSDestFile, false, false); err == nil {
		t.Fatal("required empty pin must fail")
	}
	if _, err := ResolvePinnedKey(paths, "/etc/icinga2/conf.d/factum2-hosts.conf", KeyIcingaHostsFile, true, false); err == nil {
		t.Fatal("filesystem path from the primary must fail")
	}
	if _, err := ResolvePinnedKey(paths, "not_a_key", KeyIcingaHostsFile, true, false); err == nil {
		t.Fatal("unknown key must fail")
	}

	got, err = ResolvePinnedKey(paths, KeyLegoBin, KeyLegoBin, false, true)
	if err != nil || got != "lego" {
		t.Fatalf("executable: %q %v", got, err)
	}

	bad := paths
	bad.DNSDestFile = "records"
	if _, err := ResolvePinnedKey(bad, KeyDNSDestFile, KeyDNSDestFile, false, false); err == nil {
		t.Fatal("relative pin must fail")
	}
}

func TestValidateWorkerPathKey(t *testing.T) {
	t.Parallel()
	if err := ValidateWorkerPathKey(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkerPathKey("  icinga_hosts_file  "); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkerPathKey("/factum/hosts.conf"); err == nil {
		t.Fatal("path must fail")
	}
	if err := ValidateWorkerPathKey("lego"); err == nil {
		t.Fatal("bare command must fail; the key is lego_bin")
	}
}
