package storage

import (
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestResolveRoot(t *testing.T) {
	paths := util.ConfigWorkerPaths{StorageRoot: DefaultRoot}
	got, err := ResolveRoot(paths, &Config{})
	if err != nil || got != DefaultRoot {
		t.Fatalf("empty key: %q %v", got, err)
	}
	got, err = ResolveRoot(paths, &Config{Root: util.KeyStorageRoot})
	if err != nil || got != DefaultRoot {
		t.Fatalf("named key: %q %v", got, err)
	}
	if _, err := ResolveRoot(paths, &Config{Root: "/data/images"}); err == nil {
		t.Fatal("filesystem path was accepted")
	}
	if _, err := ResolveRoot(util.ConfigWorkerPaths{}, &Config{}); err == nil {
		t.Fatal("missing pin was accepted")
	}
}
