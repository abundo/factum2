package drivers

import (
	"strings"
	"unicode"
)

// CLI prompt detection cuts the idle wait once the device has reprinted its
// prompt. The hostname in that prompt is the one configured on the box
// (Nokia LU17-LAB-R4), not the DNS name we dialed (lu17-lab-r4.itn.nu), so
// it is learned from the first prompt the session actually prints.
//
// Patterns are per platform. A line counts only when the whole line is a
// prompt, so a hostname mentioned in command output does not finish the
// wait. idleWindow remains the fallback when the prompt never shows up.

func cliPromptPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "sros", "sros-md":
		return "sros"
	case "vrp":
		return "vrp"
	case "ios-xr":
		return "ios-xr"
	case "ciscosmb", "ios", "eos":
		return "cisco"
	default:
		return ""
	}
}

func (s *sshShellSession) setCLIPlatform(platform string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.platform = cliPromptPlatform(platform)
	s.mu.Unlock()
}

// promptSatisfied reports whether buf has reached the CLI prompt often
// enough to treat the wait as finished. need is 1 for a single command
// (the buffer must end on the prompt). A pipeline paste passes the command
// count, because each line reprints the prompt and the first one is not
// the end of the paste. host is set whenever the buffer currently ends on
// a prompt, including when done is still false, so the hostname can be
// learned before the count is reached.
func promptSatisfied(platform, known, buf string, need int) (host string, done bool) {
	platform = cliPromptPlatform(platform)
	if platform == "" || need < 1 {
		return "", false
	}
	lines := splitPromptLines(buf)
	if len(lines) == 0 {
		return "", false
	}
	got, ok := matchCLIPrompt(platform, lines[len(lines)-1], known)
	if !ok {
		return "", false
	}
	if need == 1 || countCLIPromptLines(platform, got, lines) >= need {
		return got, true
	}
	return got, false
}

func matchCLIPrompt(platform, line, known string) (string, bool) {
	switch cliPromptPlatform(platform) {
	case "sros":
		return matchSROSPrompt(line, known)
	case "vrp":
		return matchVRPPrompt(line, known)
	case "ios-xr":
		return matchIOSXRPrompt(line, known)
	case "cisco":
		return matchCiscoPrompt(line, known)
	default:
		return "", false
	}
}

// matchSROSPrompt matches SR OS MD-CLI and classic CLI prompt lines.
// MD-CLI, including the lab box lu17-lab-r4.itn.nu:
//
//	[/]
//	A:admin@LU17-LAB-R4#
//
// The context line ([/], [ex:/configure], …) is not the prompt. The
// hostname line is. Classic CLI is "*A:LU17-LAB-R4#" or
// "*A:LU17-LAB-R4>config>router#".
func matchSROSPrompt(line, known string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	if host, ok := matchSROSMDPrompt(line); ok {
		return acceptPromptHost(host, known)
	}
	if host, ok := matchSROSClassicPrompt(line); ok {
		return acceptPromptHost(host, known)
	}
	return "", false
}

func matchSROSMDPrompt(line string) (string, bool) {
	// optional uncommitted/exclusive markers, CPM, user, hostname, '#'.
	rest := trimPromptMarkers(line)
	colon := strings.IndexByte(rest, ':')
	if colon != 1 {
		return "", false
	}
	cpm := rest[:colon]
	if len(cpm) != 1 || !isASCIILetter(cpm[0]) {
		return "", false
	}
	rest = rest[colon+1:]
	at := strings.IndexByte(rest, '@')
	if at <= 0 {
		return "", false
	}
	user := rest[:at]
	if user == "" || strings.ContainsAny(user, " \t#>") {
		return "", false
	}
	rest = rest[at+1:]
	if !strings.HasSuffix(rest, "#") {
		return "", false
	}
	host := strings.TrimSuffix(rest, "#")
	if !validPromptHost(host) {
		return "", false
	}
	return host, true
}

func matchSROSClassicPrompt(line string) (string, bool) {
	rest := trimPromptMarkers(line)
	colon := strings.IndexByte(rest, ':')
	if colon != 1 || !isASCIILetter(rest[0]) {
		return "", false
	}
	rest = rest[colon+1:]
	if rest == "" || strings.Contains(rest, "@") {
		return "", false
	}
	end := len(rest)
	if rest[end-1] == '#' || rest[end-1] == '>' {
		rest = rest[:end-1]
	} else {
		return "", false
	}
	host := rest
	if i := strings.IndexByte(rest, '>'); i >= 0 {
		host = rest[:i]
		ctx := rest[i:]
		if ctx == "" || strings.ContainsAny(ctx, " \t#") {
			return "", false
		}
	}
	if !validPromptHost(host) {
		return "", false
	}
	return host, true
}

// trimPromptMarkers strips the SR OS prefixes that sit in front of the
// CPM letter: '*', '!', and "(ex)" / "(gl)" / "(ro)" / "(pr)".
func trimPromptMarkers(line string) string {
	for {
		line = strings.TrimLeft(line, "*! ")
		if len(line) < 4 || line[0] != '(' {
			return line
		}
		close := strings.IndexByte(line, ')')
		if close < 2 || close > 4 {
			return line
		}
		switch line[1:close] {
		case "ex", "gl", "ro", "pr":
			line = line[close+1:]
		default:
			return line
		}
	}
}

func matchVRPPrompt(line, known string) (string, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 3 {
		return "", false
	}
	open, closeCh := line[0], line[len(line)-1]
	if !((open == '<' && closeCh == '>') || (open == '[' && closeCh == ']')) {
		return "", false
	}
	inner := line[1 : len(line)-1]
	for len(inner) > 0 && (inner[0] == '*' || inner[0] == '~') {
		inner = inner[1:]
	}
	if inner == "" || strings.ContainsAny(inner, " \t") {
		return "", false
	}
	if known != "" {
		lowerInner := strings.ToLower(inner)
		lowerHost := strings.ToLower(known)
		if lowerInner == lowerHost || strings.HasPrefix(lowerInner, lowerHost+"-") {
			return known, true
		}
		return "", false
	}
	host := inner
	if slash := strings.IndexByte(inner, '/'); slash >= 0 {
		head := inner[:slash]
		dash := strings.LastIndex(head, "-")
		if dash <= 0 {
			return "", false
		}
		host = head[:dash]
	}
	return acceptPromptHost(host, "")
}

func matchCiscoPrompt(line, known string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	last := line[len(line)-1]
	if last != '#' && last != '>' {
		return "", false
	}
	body := strings.TrimSpace(line[:len(line)-1])
	if i := strings.IndexByte(body, '('); i >= 0 {
		if !strings.HasSuffix(body, ")") {
			return "", false
		}
		mode := body[i+1 : len(body)-1]
		if mode == "" || strings.ContainsAny(mode, " \t()") {
			return "", false
		}
		body = body[:i]
	}
	if strings.ContainsAny(body, " \t:/") {
		return "", false
	}
	return acceptPromptHost(body, known)
}

func matchIOSXRPrompt(line, known string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasSuffix(line, "#") {
		return "", false
	}
	body := strings.TrimSpace(strings.TrimSuffix(line, "#"))
	if i := strings.LastIndex(body, ":"); i >= 0 {
		body = body[i+1:]
	}
	if i := strings.IndexByte(body, '('); i >= 0 {
		if !strings.HasSuffix(body, ")") {
			return "", false
		}
		mode := body[i+1 : len(body)-1]
		if mode == "" || strings.ContainsAny(mode, " \t()") {
			return "", false
		}
		body = body[:i]
	}
	if strings.ContainsAny(body, " \t:/") {
		return "", false
	}
	return acceptPromptHost(body, known)
}

func acceptPromptHost(host, known string) (string, bool) {
	if !validPromptHost(host) {
		return "", false
	}
	if known != "" && !strings.EqualFold(host, known) {
		return "", false
	}
	if known != "" {
		return known, true
	}
	return host, true
}

func validPromptHost(host string) bool {
	if host == "" || len(host) > 63 {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '_' || c == '.' || c == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func isASCIILetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func countCLIPromptLines(platform, host string, lines []string) int {
	n := 0
	prev := false
	for _, line := range lines {
		if isPromptGlue(platform, line) {
			continue
		}
		if got, ok := matchCLIPrompt(platform, line, host); ok && strings.EqualFold(got, host) {
			if !prev {
				n++
			}
			prev = true
			continue
		}
		prev = false
	}
	return n
}

// isPromptGlue keeps an SR OS context line ([/], *(ex)[/configure …]) from
// splitting one prompt redraw into two counts. Empty lines are glue too.
func isPromptGlue(platform, line string) bool {
	if strings.TrimSpace(line) == "" {
		return true
	}
	if platform != "sros" {
		return false
	}
	i := strings.IndexByte(line, '[')
	return i >= 0 && strings.HasSuffix(line, "]") && !strings.Contains(line, "@")
}

func splitPromptLines(buf string) []string {
	buf = stripTerminalControls(buf)
	buf = strings.ReplaceAll(buf, "\r\n", "\n")
	buf = strings.TrimRightFunc(buf, unicode.IsSpace)
	if buf == "" {
		return nil
	}
	raw := strings.Split(buf, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if i := strings.LastIndex(line, "\r"); i >= 0 {
			line = line[i+1:]
		}
		out = append(out, strings.TrimSpace(line))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// stripTerminalControls removes ECMA-48 CSI and OSC sequences so a colored
// prompt still matches. Bytes other than ESC are kept, including CR, which
// splitPromptLines uses to keep the redrawn prompt.
func stripTerminalControls(s string) string {
	if strings.IndexByte(s, 0x1b) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		switch s[i+1] {
		case '[':
			i += 2
			for i < len(s) {
				c := s[i]
				i++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
			i--
		case ']':
			i += 2
			for i < len(s) {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return b.String()
}
