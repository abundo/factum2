package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoPathJail(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r, err := NewRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	absOutside, apiOutside, err := r.resolve("../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if apiOutside != "/etc/passwd" {
		t.Fatalf("cleaned api %q", apiOutside)
	}
	rootClean := filepath.Clean(root)
	if absOutside != rootClean && !strings.HasPrefix(absOutside, rootClean+string(filepath.Separator)) {
		t.Fatalf("escaped root: %q", absOutside)
	}
	if _, _, err := r.resolve("/.ssh/host_key"); err != ErrNotFound {
		t.Fatalf("hidden path: %v", err)
	}
	abs, api, err := r.resolve("/eos/EOS.swi")
	if err != nil {
		t.Fatal(err)
	}
	if api != "/eos/EOS.swi" {
		t.Fatalf("api path %q", api)
	}
	if !strings.HasPrefix(abs, filepath.Clean(root)) {
		t.Fatalf("abs %q not under root %q", abs, root)
	}
}

func TestRepoCRUD(t *testing.T) {
	t.Parallel()
	r, err := NewRepo(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir("/eos"); err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir("/eos"); err != ErrExists {
		t.Fatalf("mkdir exists: %v", err)
	}
	n, err := r.WriteAt("/eos/a.bin", 0, strings.NewReader("hello"))
	if err != nil || n != 5 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	ents, err := r.List("/eos")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name != "a.bin" || ents[0].IsDir || ents[0].Size != 5 {
		t.Fatalf("list: %+v", ents)
	}
	if err := r.Move("/eos/a.bin", "/eos/b.bin"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Stat("/eos/a.bin"); err != ErrNotFound {
		t.Fatalf("old path: %v", err)
	}
	if err := r.Remove("/eos"); err != ErrNotEmpty {
		t.Fatalf("remove dir: %v", err)
	}
	if err := r.Remove("/eos/b.bin"); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove("/eos"); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove("/"); err == nil {
		t.Fatal("deleted root")
	}
}

func TestRepoHidesDotFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ssh", "host_key"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	ents, err := r.List("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("listed hidden: %+v", ents)
	}
}
