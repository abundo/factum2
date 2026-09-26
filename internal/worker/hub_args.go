package worker

import (
	"fmt"
	"strings"
	"unicode"
)

// filterWireArgs keeps cmdMsg.Args only when every token is listed in allow.
// An allow entry of "--path=*" accepts "--path" plus one following value.
// An entry without "=*" must match the token exactly. An empty allow list
// accepts no wire arguments.
func filterWireArgs(allow, wire []string) ([]string, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	exact := make(map[string]bool, len(allow))
	valued := make(map[string]bool)
	for _, a := range allow {
		if strings.HasSuffix(a, "=*") {
			valued[strings.TrimSuffix(a, "=*")] = true
			continue
		}
		exact[a] = true
	}
	out := make([]string, 0, len(wire))
	for i := 0; i < len(wire); i++ {
		tok := wire[i]
		if valued[tok] {
			if i+1 >= len(wire) {
				return nil, fmt.Errorf("missing value for %s", tok)
			}
			val := wire[i+1]
			if !safeWireValue(val) {
				return nil, fmt.Errorf("rejected value for %s", tok)
			}
			out = append(out, tok, val)
			i++
			continue
		}
		if exact[tok] {
			out = append(out, tok)
			continue
		}
		return nil, fmt.Errorf("argument %q is not allowed", tok)
	}
	return out, nil
}

func safeWireValue(s string) bool {
	if s == "" || len(s) > 1024 || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
