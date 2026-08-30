package drivers

import (
	"bytes"
	"strings"
	"text/template"
)

// renderELINETemplate renders the root of an ELINE config template (e.g.
// templates/eos_eline.tmpl) against data and splits the result into one CLI
// command per line, dropping blank lines left behind by template
// conditionals - the shape eapiRunCmds/sshRunCLI want, one command per
// slice element. data is usually a *ELINEIntent, but a driver that needs
// extra template-only fields (e.g. SR OS's derived SDP ID - see
// driver_nokia_sros_eline.go) can pass a wrapper struct embedding one
// instead, hence the loose type rather than *ELINEIntent specifically.
//
// Platform templates own both cleanup and desired-state CLI: the root
// template typically {{template "cleanup" .}} then the apply body, so a
// full ApplyELINE is one render. RemoveELINE uses renderELINETemplateDefine
// with name "cleanup" so tear-down shares the same source of truth.
func renderELINETemplate(tmplText string, data any) ([]string, error) {
	return renderELINETemplateDefine(tmplText, "", data)
}

// RenderCLITemplate is the CLI line renderer used by ELINE apply/remove.
func RenderCLITemplate(tmplText string, data any) ([]string, error) {
	return renderELINETemplate(tmplText, data)
}

// renderELINETemplateDefine is like renderELINETemplate but executes the
// named define (e.g. "cleanup") instead of the root template. An empty
// name executes the root, matching renderELINETemplate.
func renderELINETemplateDefine(tmplText string, name string, data any) ([]string, error) {
	tmpl, err := template.New("eline").Parse(tmplText)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if name == "" {
		err = tmpl.Execute(&buf, data)
	} else {
		err = tmpl.ExecuteTemplate(&buf, name, data)
	}
	if err != nil {
		return nil, err
	}
	var cmds []string
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			cmds = append(cmds, line)
		}
	}
	return cmds, nil
}
