// Package docs embeds operator-facing Markdown from docs/user/ so the
// running factum2-web binary can serve the same pages as GitHub Pages.
// Install and design notes stay on disk for the public site / git; they
// are not embedded. A -tags release build overlays docs/generated/sbom.md
// onto the sbom page when that file was produced by `make sbom`.
package docs

import (
	"embed"
	"errors"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed user/*.md
var userFS embed.FS

var (
	// ErrInvalidSlug is returned when a requested slug is not a lowercase
	// hyphenated name (so path traversal never reaches ReadFile).
	ErrInvalidSlug = errors.New("invalid slug")
	// ErrNotFound is returned when no user doc matches a valid slug.
	ErrNotFound = errors.New("not found")
)

// Page is one operator doc. List omits Markdown; Get includes it.
type Page struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Order    int    `json:"-"`
	Markdown string `json:"markdown,omitempty"`
}

// List returns every docs/user/*.md page, ordered by front-matter `order`
// then title, without Markdown bodies.
func List() ([]Page, error) {
	entries, err := fs.ReadDir(userFS, "user")
	if err != nil {
		return nil, err
	}
	pages := make([]Page, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		if !validSlug(slug) {
			continue
		}
		raw, err := userFS.ReadFile("user/" + e.Name())
		if err != nil {
			return nil, err
		}
		p := parsePage(slug, raw)
		p.Markdown = ""
		pages = append(pages, p)
	}
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Order != pages[j].Order {
			return pages[i].Order < pages[j].Order
		}
		return pages[i].Title < pages[j].Title
	})
	return pages, nil
}

// Get returns one user doc by slug, including Markdown with front matter
// stripped.
func Get(slug string) (Page, error) {
	if !validSlug(slug) {
		return Page{}, ErrInvalidSlug
	}
	raw, err := userFS.ReadFile("user/" + slug + ".md")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Page{}, ErrNotFound
		}
		return Page{}, err
	}
	return overlaySBOM(parsePage(slug, raw)), nil
}

func validSlug(slug string) bool {
	if slug == "" {
		return false
	}
	for i, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		case r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func parsePage(slug string, raw []byte) Page {
	meta, body := splitFrontMatter(string(raw))
	p := Page{
		Slug:     slug,
		Order:    1000,
		Markdown: strings.TrimSpace(body) + "\n",
	}
	if t := strings.TrimSpace(meta["title"]); t != "" {
		p.Title = t
	} else {
		p.Title = headingTitle(body, slug)
	}
	if o := strings.TrimSpace(meta["order"]); o != "" {
		if n, err := strconv.Atoi(o); err == nil {
			p.Order = n
		}
	}
	return p
}

func splitFrontMatter(text string) (map[string]string, string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, text
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, text
	}
	fm := rest[:end]
	body := rest[end+len("\n---\n"):]
	meta := map[string]string{}
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return meta, body
}

func headingTitle(body, slug string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return slug
}
