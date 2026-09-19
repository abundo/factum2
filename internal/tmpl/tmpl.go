// Package tmpl executes operator-facing templates with Jet (CloudyKit).
// HTML email bodies stay on html/template for auto-escaping.
package tmpl

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/CloudyKit/jet/v6"
)

const maxIncludeDepth = 8

// Options control a single Execute.
type Options struct {
	// Name is the template path used in error messages.
	Name string
	// Block, if set, imports the body and yields that named block only.
	Block string
	// Macros are named snippets available to {{ include "name" }} and include("name").
	Macros map[string]string
	// Funcs are extra globals (Go funcs or jet.Func).
	Funcs map[string]any
}

// Parse checks that body is valid Jet without executing it.
func Parse(body string, opt Options) error {
	_, err := parseTemplate(body, nil, opt, 0)
	return err
}

// Execute parses body as Jet and renders it against data. HTML escaping is
// off so CLI and Icinga DSL are copied verbatim. Missing struct fields
// error; missing map keys render empty (use isset).
func Execute(body string, data any, opt Options) (string, error) {
	return execute(body, data, opt, 0)
}

func execute(body string, data any, opt Options, depth int) (string, error) {
	t, err := parseTemplate(body, data, opt, depth)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, nil, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func parseTemplate(body string, data any, opt Options, depth int) (*jet.Template, error) {
	if depth > maxIncludeDepth {
		return nil, fmt.Errorf("macro include nested too deeply")
	}
	name := strings.TrimSpace(opt.Name)
	if name == "" {
		name = "blob"
	}
	loader := jet.NewInMemLoader()
	for macroName, src := range opt.Macros {
		if macroName == "" {
			continue
		}
		loader.Set(macroName, src)
	}
	set := jet.NewSet(loader, jet.WithSafeWriter(nil))
	addFuncs(set, data, opt, depth)

	if strings.TrimSpace(opt.Block) != "" {
		if !blockNameOK(opt.Block) {
			return nil, fmt.Errorf("invalid template block name %q", opt.Block)
		}
		loader.Set(name, body)
		wrapper := fmt.Sprintf("{{ import %q }}\n{{ yield %s() }}", name, opt.Block)
		return set.Parse(name+"-"+opt.Block, wrapper)
	}
	return set.Parse(name, body)
}

func addFuncs(set *jet.Set, data any, opt Options, depth int) {
	set.AddGlobalFunc("join", joinFunc)
	set.AddGlobalFunc("eq", eqFunc)
	set.AddGlobalFunc("ne", neFunc)
	set.AddGlobalFunc("include", func(args jet.Arguments) reflect.Value {
		args.RequireNumOfArguments("include", 1, 1)
		macroName := fmt.Sprint(args.Get(0).Interface())
		src, ok := opt.Macros[macroName]
		if !ok {
			args.Panicf("unknown macro %q", macroName)
		}
		out, err := execute(src, data, Options{Name: macroName, Macros: opt.Macros, Funcs: opt.Funcs}, depth+1)
		if err != nil {
			args.Panicf("%v", err)
		}
		return reflect.ValueOf(out)
	})
	for k, v := range opt.Funcs {
		if fn, ok := v.(jet.Func); ok {
			set.AddGlobalFunc(k, fn)
			continue
		}
		set.AddGlobal(k, v)
	}
}

func blockNameOK(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func joinFunc(args jet.Arguments) reflect.Value {
	args.RequireNumOfArguments("join", 2, 2)
	sep := fmt.Sprint(args.Get(0).Interface())
	list := derefValue(args.Get(1))
	if !list.IsValid() {
		return reflect.ValueOf("")
	}
	switch list.Kind() {
	case reflect.Slice, reflect.Array:
		parts := make([]string, list.Len())
		for i := 0; i < list.Len(); i++ {
			parts[i] = fmt.Sprint(list.Index(i).Interface())
		}
		return reflect.ValueOf(strings.Join(parts, sep))
	default:
		args.Panicf("join: second argument must be a list, got %s", list.Kind())
		return reflect.Value{}
	}
}

func derefValue(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func eqFunc(args jet.Arguments) reflect.Value {
	args.RequireNumOfArguments("eq", 2, 2)
	return reflect.ValueOf(fmt.Sprint(args.Get(0).Interface()) == fmt.Sprint(args.Get(1).Interface()))
}

func neFunc(args jet.Arguments) reflect.Value {
	args.RequireNumOfArguments("ne", 2, 2)
	return reflect.ValueOf(fmt.Sprint(args.Get(0).Interface()) != fmt.Sprint(args.Get(1).Interface()))
}

// StringErrFunc wraps func(string) (string, error) as a Jet function.
func StringErrFunc(name string, fn func(string) (string, error)) jet.Func {
	return func(args jet.Arguments) reflect.Value {
		args.RequireNumOfArguments(name, 1, 1)
		in := fmt.Sprint(args.Get(0).Interface())
		out, err := fn(in)
		if err != nil {
			args.Panicf("%s: %v", name, err)
		}
		return reflect.ValueOf(out)
	}
}

// StringFunc wraps func(string) string as a Jet function.
func StringFunc(name string, fn func(string) string) jet.Func {
	return func(args jet.Arguments) reflect.Value {
		args.RequireNumOfArguments(name, 1, 1)
		return reflect.ValueOf(fn(fmt.Sprint(args.Get(0).Interface())))
	}
}

// SplitCLI splits rendered text into non-empty trimmed lines.
func SplitCLI(text string) []string {
	var cmds []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			cmds = append(cmds, line)
		}
	}
	return cmds
}
