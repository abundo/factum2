package sbom

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const modulePath = "github.com/abundo/factum2"

// Generate runs `go list -m -json all` and reads the GUI lockfile under
// repoRoot, then renders the inventory markdown. Empty Meta fields are
// filled from git / the running Go toolchain.
func Generate(repoRoot string, meta Meta) (string, error) {
	if repoRoot == "" {
		return "", fmt.Errorf("empty repo root")
	}
	meta, err := fillMeta(repoRoot, meta)
	if err != nil {
		return "", err
	}
	modules, err := listGoModules(repoRoot)
	if err != nil {
		return "", err
	}
	lockPath := filepath.Join(repoRoot, "web", "frontend", "package-lock.json")
	lock, err := os.ReadFile(lockPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", lockPath, err)
	}
	npm, err := ParseNPMLock(lock)
	if err != nil {
		return "", err
	}
	return Markdown(meta, modules, npm), nil
}

// FindRepoRoot walks up from dir (or the process cwd) until it finds this
// module's go.mod.
func FindRepoRoot(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && hasModuleDecl(body, modulePath) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside %s (no go.mod found walking up from %s)", modulePath, dir)
		}
		dir = parent
	}
}

func hasModuleDecl(goMod []byte, path string) bool {
	want := "module " + path
	for _, line := range strings.Split(string(goMod), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func listGoModules(repoRoot string) ([]GoModule, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		return nil, fmt.Errorf("go list -m -json all: %w\n%s", err, stderr)
	}
	return ParseGoListJSON(bytes.NewReader(out))
}

func fillMeta(repoRoot string, meta Meta) (Meta, error) {
	if strings.TrimSpace(meta.Version) == "" {
		v, err := git(repoRoot, "describe", "--tags", "--always", "--dirty")
		if err != nil {
			v = "dev"
		}
		meta.Version = v
	}
	if strings.TrimSpace(meta.Commit) == "" {
		c, err := git(repoRoot, "rev-parse", "HEAD")
		if err != nil {
			c = "none"
		}
		meta.Commit = c
	}
	if strings.TrimSpace(meta.Date) == "" {
		d, err := git(repoRoot, "log", "-1", "--format=%cI")
		if err != nil {
			d = "unknown"
		}
		meta.Date = d
	}
	if strings.TrimSpace(meta.GoVersion) == "" {
		meta.GoVersion = runtime.Version()
	}
	return meta, nil
}

func git(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
