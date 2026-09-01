package oxidized

//
// REST API client for oxidized-web, used by the factum GUI's Oxidized
// device browser (web.ApiOxidized*). oxidized-web is required: the core
// oxidized process has no HTTP API of its own.
//
// Deleted / obsolete devices are not available through this API. /nodes
// only lists the current source (router.db), and version/fetch/diff look
// the node up in that live list first (NodeNotFound otherwise). Git still
// keeps historical blobs unless output.clean_obsolete_nodes is enabled,
// but oxidized-web does not expose a way to list or fetch those files.
//

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// ErrNotConfigured is returned when Settings.OxidizedApiURL is empty.
var ErrNotConfigured = fmt.Errorf("oxidized API URL is not configured")

// Node is one entry from GET /nodes.json after oxidized-web flattens
// last-status onto the row.
type Node struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	IP       string `json:"ip"`
	Group    string `json:"group"`
	Model    string `json:"model"`
	Status   string `json:"status"`
	Time     string `json:"time"`
	Mtime    string `json:"mtime"`
}

// Version is one git commit that touched a node's config
// (GET /node/version.json).
type Version struct {
	OID     string        `json:"oid"`
	Date    string        `json:"date"`
	Time    string        `json:"time"`
	Author  VersionAuthor `json:"author"`
	Message string        `json:"message"`
}

// VersionAuthor is the git committer oxidized recorded for a version.
type VersionAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Diff is a unified patch between two versions of one node's config.
type Diff struct {
	Patch   string `json:"patch"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

type rawNode struct {
	Name     string          `json:"name"`
	FullName string          `json:"full_name"`
	IP       string          `json:"ip"`
	Group    string          `json:"group"`
	Model    string          `json:"model"`
	Status   string          `json:"status"`
	Time     json.RawMessage `json:"time"`
	Mtime    json.RawMessage `json:"mtime"`
}

type rawVersion struct {
	OID     string          `json:"oid"`
	Date    json.RawMessage `json:"date"`
	Time    json.RawMessage `json:"time"`
	Author  json.RawMessage `json:"author"`
	Message string          `json:"message"`
}

type rawAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (o *oxidizedClient) baseURL() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(o.c.URL), "/")
	if base == "" {
		return "", ErrNotConfigured
	}
	return base, nil
}

func (o *oxidizedClient) apiURL(path string, query url.Values) (string, error) {
	base, err := o.baseURL()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u, nil
}

func (o *oxidizedClient) getBody(path string, query url.Values) ([]byte, int, error) {
	u, err := o.apiURL(path, query)
	if err != nil {
		return nil, 0, err
	}
	resp, err := o.get(u)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func (o *oxidizedClient) getJSON(path string, query url.Values, dest any) error {
	body, status, err := o.getBody(path, query)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("oxidized %s: HTTP %d: %s", path, status, truncateErr(body))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("oxidized %s: decode JSON: %w", path, err)
	}
	return nil
}

func truncateErr(body []byte) string {
	s := strings.TrimSpace(string(body))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 240 {
		return s[:240] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

func jsonAsString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// SplitNodeFull mirrors oxidized-web's node_full parsing: the last '/'
// separates group from node, so groups themselves may contain slashes.
func SplitNodeFull(nodeFull string) (group, node string) {
	nodeFull = strings.TrimSpace(nodeFull)
	if i := strings.LastIndex(nodeFull, "/"); i >= 0 {
		return nodeFull[:i], nodeFull[i+1:]
	}
	return "", nodeFull
}

// ListNodes is GET /nodes.json - currently configured devices only.
func (o *oxidizedClient) ListNodes() ([]Node, error) {
	var raw []rawNode
	if err := o.getJSON("/nodes.json", nil, &raw); err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(raw))
	for _, r := range raw {
		full := r.FullName
		if full == "" {
			full = r.Name
			if r.Group != "" && r.Group != "default" {
				full = r.Group + "/" + r.Name
			}
		}
		nodes = append(nodes, Node{
			Name:     r.Name,
			FullName: full,
			IP:       r.IP,
			Group:    r.Group,
			Model:    r.Model,
			Status:   r.Status,
			Time:     jsonAsString(r.Time),
			Mtime:    jsonAsString(r.Mtime),
		})
	}
	return nodes, nil
}

// FetchConfig is GET /node/fetch/[group/]node - latest stored config.
func (o *oxidizedClient) FetchConfig(nodeFull string) (string, error) {
	group, node := SplitNodeFull(nodeFull)
	if node == "" {
		return "", fmt.Errorf("missing node name")
	}
	path := "/node/fetch/" + url.PathEscape(node)
	if group != "" {
		var parts []string
		for _, p := range strings.Split(group, "/") {
			parts = append(parts, url.PathEscape(p))
		}
		path = "/node/fetch/" + strings.Join(parts, "/") + "/" + url.PathEscape(node)
	}
	body, status, err := o.getBody(path, nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("oxidized fetch %s: HTTP %d: %s", nodeFull, status, truncateErr(body))
	}
	text := string(body)
	if isOxidizedMissing(text) {
		return "", fmt.Errorf("oxidized fetch %s: %s", nodeFull, strings.TrimSpace(text))
	}
	return text, nil
}

func isOxidizedMissing(text string) bool {
	s := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(s, "node not found") ||
		strings.Contains(s, "unable to find") ||
		s == "version not found" ||
		s == "no diffs" ||
		s == "no diff available"
}

// ListVersions is GET /node/version.json?node_full=...
func (o *oxidizedClient) ListVersions(nodeFull string) ([]Version, error) {
	if strings.TrimSpace(nodeFull) == "" {
		return nil, fmt.Errorf("missing node name")
	}
	var raw []rawVersion
	q := url.Values{"node_full": {nodeFull}}
	if err := o.getJSON("/node/version.json", q, &raw); err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(raw))
	for _, r := range raw {
		var author VersionAuthor
		if len(r.Author) > 0 && string(r.Author) != "null" {
			var a rawAuthor
			if err := json.Unmarshal(r.Author, &a); err == nil {
				author = VersionAuthor{Name: a.Name, Email: a.Email}
			}
		}
		versions = append(versions, Version{
			OID:     r.OID,
			Date:    jsonAsString(r.Date),
			Time:    jsonAsString(r.Time),
			Author:  author,
			Message: r.Message,
		})
	}
	return versions, nil
}

// GetVersion is GET /node/version/view?format=text - one historical blob.
func (o *oxidizedClient) GetVersion(nodeFull, oid string) (string, error) {
	group, node := SplitNodeFull(nodeFull)
	if node == "" {
		return "", fmt.Errorf("missing node name")
	}
	if oid == "" {
		return "", fmt.Errorf("missing version oid")
	}
	q := url.Values{
		"node":   {node},
		"group":  {group},
		"oid":    {oid},
		"format": {"text"},
	}
	body, status, err := o.getBody("/node/version/view", q)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("oxidized version %s@%s: HTTP %d: %s", nodeFull, oid, status, truncateErr(body))
	}
	text := string(body)
	if isOxidizedMissing(text) {
		return "", fmt.Errorf("oxidized version %s@%s: %s", nodeFull, oid, strings.TrimSpace(text))
	}
	return text, nil
}

// GetDiff is GET /node/version/diffs?format=json. oid2 empty diffs oid
// against its parent commit (oxidized-web's default).
func (o *oxidizedClient) GetDiff(nodeFull, oid, oid2 string) (*Diff, error) {
	group, node := SplitNodeFull(nodeFull)
	if node == "" {
		return nil, fmt.Errorf("missing node name")
	}
	if oid == "" {
		return nil, fmt.Errorf("missing version oid")
	}
	q := url.Values{
		"node":   {node},
		"group":  {group},
		"oid":    {oid},
		"format": {"json"},
	}
	if oid2 != "" {
		q.Set("oid2", oid2)
	}
	body, status, err := o.getBody("/node/version/diffs", q)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("oxidized diff %s: HTTP %d: %s", nodeFull, status, truncateErr(body))
	}
	patch, err := decodeDiffBody(body)
	if err != nil {
		return nil, fmt.Errorf("oxidized diff %s: %w", nodeFull, err)
	}
	if isOxidizedMissing(patch) {
		return &Diff{Patch: ""}, nil
	}
	return &Diff{
		Patch:   patch,
		Added:   countDiffPrefix(patch, "+"),
		Removed: countDiffPrefix(patch, "-"),
	}, nil
}

func decodeDiffBody(body []byte) (string, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var lines []string
		if err := json.Unmarshal(body, &lines); err != nil {
			return "", err
		}
		return strings.Join(lines, ""), nil
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			return "", err
		}
		if patch, ok := obj["patch"].(string); ok {
			return patch, nil
		}
	}
	var s string
	if err := json.Unmarshal(body, &s); err == nil {
		return s, nil
	}
	return string(body), nil
}

func countDiffPrefix(patch, prefix string) int {
	n := 0
	for _, line := range strings.Split(patch, "\n") {
		if prefix == "+" {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				n++
			}
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			n++
		}
	}
	return n
}
