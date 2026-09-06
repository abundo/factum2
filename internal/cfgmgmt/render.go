package cfgmgmt

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

var cleanupInvokeRe = regexp.MustCompile(`\{\{-?\s*template\s+"cleanup"\s+[^}]*\}\}`)

var defineStartRe = regexp.MustCompile(`\{\{-?\s*define\s+(?:"([^"]+)"|` + "`([^`]+)`" + `)\s*-?\}\}`)

// extractDefineBody returns the inner source of {{define "name"}}…{{end}},
// counting nested if/range/with/block/define so a cleanup that contains
// {{range}}…{{end}} is not truncated at the first end.
func extractDefineBody(src, name string) string {
	if src == "" || name == "" {
		return ""
	}
	locs := defineStartRe.FindAllStringSubmatchIndex(src, -1)
	start := -1
	for _, loc := range locs {
		got := ""
		if loc[2] >= 0 {
			got = src[loc[2]:loc[3]]
		} else if loc[4] >= 0 {
			got = src[loc[4]:loc[5]]
		}
		if got == name {
			start = loc[1]
			break
		}
	}
	if start < 0 {
		return ""
	}
	depth := 1
	i := start
	for i < len(src) {
		j := strings.Index(src[i:], "{{")
		if j < 0 {
			break
		}
		j += i
		k := strings.Index(src[j:], "}}")
		if k < 0 {
			break
		}
		end := j + k + 2
		verb := templateActionVerb(src[j+2 : j+k])
		switch verb {
		case "if", "range", "with", "block", "define":
			depth++
		case "end":
			depth--
			if depth == 0 {
				return src[start:j]
			}
		}
		i = end
	}
	return ""
}

func templateActionVerb(action string) string {
	s := strings.TrimSpace(action)
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "/*") {
		return "comment"
	}
	if strings.HasSuffix(s, "-") {
		s = strings.TrimSpace(s[:len(s)-1])
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func packToCLIBlobs(apply, cleanup string) (add, remove string) {
	add = cleanupInvokeRe.ReplaceAllString(apply, "")
	if strings.TrimSpace(cleanup) != "" {
		remove = cleanup
	} else {
		remove = extractDefineBody(apply, "cleanup")
	}
	return add, remove
}

const maxIncludeDepth = 8

// Render executes Go text/template body (or a named define) against data.
// FuncMap is limited: include (named ConfigMacro), join. No file/HTTP/shell.
func Render(db *gorm.DB, body, define string, data any) ([]string, error) {
	text, err := executeTemplate(db, body, define, data, 0)
	if err != nil {
		return nil, err
	}
	return splitCLI(text), nil
}

func executeTemplate(db *gorm.DB, body, define string, data any, depth int) (string, error) {
	if depth > maxIncludeDepth {
		return "", fmt.Errorf("macro include nested too deeply")
	}
	funcs := template.FuncMap{
		"join": strings.Join,
		"include": func(name string) (string, error) {
			var m models.ConfigMacro
			if err := db.Where("name = ?", name).First(&m).Error; err != nil {
				return "", fmt.Errorf("unknown macro %q", name)
			}
			return executeTemplate(db, m.Body, "", data, depth+1)
		},
		"eq": eqAny,
		"ne": func(a, b any) bool { return !eqAny(a, b) },
	}
	tmpl, err := template.New("cfg").Funcs(funcs).Option("missingkey=error").Parse(body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if define == "" {
		err = tmpl.Execute(&buf, data)
	} else {
		err = tmpl.ExecuteTemplate(&buf, define, data)
	}
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func eqAny(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func splitCLI(text string) []string {
	var cmds []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			cmds = append(cmds, line)
		}
	}
	return cmds
}
