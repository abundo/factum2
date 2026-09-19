package cfgmgmt

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/abundo/factum2/internal/tmpl"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

var cleanupInvokeRe = regexp.MustCompile(`\{\{-?\s*(?:template\s+"cleanup"\s+[^}]*|yield\s+cleanup\s*\([^)]*\))\s*-?\}\}`)

var defineStartRe = regexp.MustCompile(`\{\{-?\s*(?:define\s+(?:"([^"]+)"|` + "`([^`]+)`" + `)|block\s+([A-Za-z_][\w]*)\s*\([^)]*\))\s*-?\}\}`)

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
		} else if loc[6] >= 0 {
			got = src[loc[6]:loc[7]]
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

// Render executes a Jet template body (or a named block) against data.
// Funcs: include, join, eq/ne, sdpid, macColon/macHyphen/macCisco.
// No file/HTTP/shell. Missing struct fields error; missing map keys are empty.
func Render(db *gorm.DB, body, define string, data any) ([]string, error) {
	text, err := executeTemplate(db, body, define, data)
	if err != nil {
		return nil, err
	}
	return splitCLI(text), nil
}

func executeTemplate(db *gorm.DB, body, define string, data any) (string, error) {
	macros := map[string]string{}
	if db != nil {
		var rows []models.ConfigMacro
		if err := db.Find(&rows).Error; err != nil {
			return "", err
		}
		for _, m := range rows {
			macros[m.Name] = m.Body
		}
	}
	return tmpl.Execute(body, data, tmpl.Options{
		Name:   "cfg",
		Block:  define,
		Macros: macros,
		Funcs: map[string]any{
			"sdpid":     tmpl.StringErrFunc("sdpid", sdpidString),
			"macColon":  tmpl.StringErrFunc("macColon", macColon),
			"macHyphen": tmpl.StringErrFunc("macHyphen", macHyphen),
			"macCisco":  tmpl.StringErrFunc("macCisco", macCisco),
		},
	})
}

func sdpidString(neighborIP string) (string, error) {
	n, err := SDPIDFromNeighbor(neighborIP)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", n), nil
}

func macHex(s string) (string, error) {
	canon, err := canonicalMAC(s)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(canon, ".", ""), nil
}

func macColon(s string) (string, error) {
	h, err := macHex(s)
	if err != nil {
		return "", err
	}
	return h[0:2] + ":" + h[2:4] + ":" + h[4:6] + ":" + h[6:8] + ":" + h[8:10] + ":" + h[10:12], nil
}

func macHyphen(s string) (string, error) {
	h, err := macHex(s)
	if err != nil {
		return "", err
	}
	return h[0:2] + "-" + h[2:4] + "-" + h[4:6] + "-" + h[6:8] + "-" + h[8:10] + "-" + h[10:12], nil
}

func macCisco(s string) (string, error) {
	return canonicalMAC(s)
}

func splitCLI(text string) []string {
	return tmpl.SplitCLI(text)
}
