package sbom

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type goListModule struct {
	Path     string        `json:"Path"`
	Version  string        `json:"Version"`
	Main     bool          `json:"Main"`
	Indirect bool          `json:"Indirect"`
	Replace  *goListModule `json:"Replace"`
}

type npmLockfile struct {
	Packages map[string]npmLockPackage `json:"packages"`
}

type npmLockPackage struct {
	Version         string            `json:"version"`
	License         json.RawMessage   `json:"license"`
	Dev             bool              `json:"dev"`
	Link            bool              `json:"link"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// ParseGoListJSON reads concatenated JSON objects from `go list -m -json all`
// and returns every module except the main module.
func ParseGoListJSON(r io.Reader) ([]GoModule, error) {
	dec := json.NewDecoder(r)
	var out []GoModule
	for {
		var m goListModule
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("go list json: %w", err)
		}
		if m.Main || m.Path == "" {
			continue
		}
		gm := GoModule{
			Path:     m.Path,
			Version:  m.Version,
			Indirect: m.Indirect,
		}
		if m.Replace != nil {
			gm.ReplacePath = m.Replace.Path
			gm.ReplaceVersion = m.Replace.Version
		}
		out = append(out, gm)
	}
	return out, nil
}

// ParseNPMLock reads an npm lockfile v2/v3 `packages` map and returns
// every non-link package, unique by name@version.
func ParseNPMLock(data []byte) ([]NPMPackage, error) {
	var lock npmLockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("package-lock.json: %w", err)
	}
	if len(lock.Packages) == 0 {
		return nil, fmt.Errorf("package-lock.json: no packages (need lockfileVersion 2 or 3)")
	}
	prodDirect := map[string]bool{}
	devDirect := map[string]bool{}
	if root, ok := lock.Packages[""]; ok {
		for name := range root.Dependencies {
			prodDirect[name] = true
		}
		for name := range root.DevDependencies {
			devDirect[name] = true
		}
	}
	seen := map[string]bool{}
	var out []NPMPackage
	for key, pkg := range lock.Packages {
		if key == "" || pkg.Link {
			continue
		}
		name := npmNameFromLockKey(key)
		if name == "" || pkg.Version == "" {
			continue
		}
		id := name + "@" + pkg.Version
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, NPMPackage{
			Name:    name,
			Version: pkg.Version,
			License: parseLicense(pkg.License),
			Tree:    npmTree(name, pkg.Dev, prodDirect, devDirect),
		})
	}
	return out, nil
}

func npmTree(name string, lockDev bool, prodDirect, devDirect map[string]bool) string {
	switch {
	case prodDirect[name]:
		return "direct"
	case devDirect[name]:
		return "development"
	case lockDev:
		return "development (transitive)"
	default:
		return "transitive"
	}
}

func npmNameFromLockKey(key string) string {
	const prefix = "node_modules/"
	name := key
	for {
		i := strings.LastIndex(name, "/"+prefix)
		if i < 0 {
			break
		}
		name = name[i+len("/"+prefix):]
	}
	return strings.TrimPrefix(name, prefix)
}

func parseLicense(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if t, ok := obj["type"].(string); ok {
			return t
		}
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil {
		parts := make([]string, 0, len(arr))
		for _, item := range arr {
			switch t := item.(type) {
			case string:
				if t != "" {
					parts = append(parts, t)
				}
			case map[string]any:
				if s, ok := t["type"].(string); ok && s != "" {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, " OR ")
	}
	return ""
}
