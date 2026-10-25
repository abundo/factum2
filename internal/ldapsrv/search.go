package ldapsrv

import "strings"

const (
	filtAnd = iota
	filtOr
	filtNot
	filtEq
	filtSub
	filtGe
	filtLe
	filtPresent
	filtApprox
	filtExt
)

const (
	subInitial = iota
	subAny
	subFinal
)

type filter struct {
	op       int
	children []filter
	attr     string
	value    string
	subs     []substr
	rule     string
}

type substr struct {
	kind int
	val  string
}

func parseFilter(el ber) (filter, error) {
	if el.cls != 2 {
		return filter{}, errBER
	}
	switch el.tag {
	case 0, 1: // and, or
		f := filter{op: filtAnd}
		if el.tag == 1 {
			f.op = filtOr
		}
		for _, kid := range el.kids {
			c, err := parseFilter(kid)
			if err != nil {
				return filter{}, err
			}
			f.children = append(f.children, c)
		}
		return f, nil
	case 2: // not
		if len(el.kids) != 1 {
			return filter{}, errBER
		}
		c, err := parseFilter(el.kids[0])
		if err != nil {
			return filter{}, err
		}
		return filter{op: filtNot, children: []filter{c}}, nil
	case 3, 5, 6, 8: // equality, ge, le, approx
		if len(el.kids) != 2 {
			return filter{}, errBER
		}
		op := filtEq
		switch el.tag {
		case 5:
			op = filtGe
		case 6:
			op = filtLe
		case 8:
			op = filtApprox
		}
		return filter{op: op, attr: el.kids[0].text(), value: el.kids[1].text()}, nil
	case 4: // substrings
		if len(el.kids) != 2 {
			return filter{}, errBER
		}
		f := filter{op: filtSub, attr: el.kids[0].text()}
		for _, part := range el.kids[1].kids {
			if part.cls != 2 {
				return filter{}, errBER
			}
			kind := subAny
			switch part.tag {
			case 0:
				kind = subInitial
			case 2:
				kind = subFinal
			}
			f.subs = append(f.subs, substr{kind: kind, val: part.text()})
		}
		return f, nil
	case 7: // present
		return filter{op: filtPresent, attr: el.text()}, nil
	case 9: // extensible
		f := filter{op: filtExt}
		for _, kid := range el.kids {
			if kid.cls != 2 {
				continue
			}
			switch kid.tag {
			case 1:
				f.rule = kid.text()
			case 2:
				f.attr = kid.text()
			case 3:
				f.value = kid.text()
			}
		}
		return f, nil
	default:
		return filter{}, errBER
	}
}

func (d *Directory) match(e *Entry, f filter) bool {
	switch f.op {
	case filtAnd:
		for _, c := range f.children {
			if !d.match(e, c) {
				return false
			}
		}
		return true
	case filtOr:
		for _, c := range f.children {
			if d.match(e, c) {
				return true
			}
		}
		return false
	case filtNot:
		if len(f.children) != 1 {
			return false
		}
		return !d.match(e, f.children[0])
	case filtPresent:
		_, ok := e.Attrs[strings.ToLower(f.attr)]
		return ok
	case filtEq, filtApprox:
		return eqAttr(e, f.attr, f.value)
	case filtGe:
		return cmpAttr(e, f.attr, f.value, true)
	case filtLe:
		return cmpAttr(e, f.attr, f.value, false)
	case filtSub:
		return subAttr(e, f.attr, f.subs)
	case filtExt:
		if strings.EqualFold(f.rule, MatchingRuleInChain) {
			return d.inChain(e, f.attr, f.value)
		}
		if f.attr == "" {
			return false
		}
		return eqAttr(e, f.attr, f.value)
	default:
		return false
	}
}

func eqAttr(e *Entry, attr, value string) bool {
	attr = strings.ToLower(attr)
	vals := e.Attrs[attr]
	dnSyntax := attr == "member" || attr == "memberof" || attr == "distinguishedname"
	for _, v := range vals {
		if dnSyntax {
			if normalizeDN(v) == normalizeDN(value) {
				return true
			}
			continue
		}
		if strings.EqualFold(v, value) {
			return true
		}
	}
	return false
}

func cmpAttr(e *Entry, attr, value string, ge bool) bool {
	value = strings.ToLower(value)
	for _, v := range e.Attrs[strings.ToLower(attr)] {
		c := strings.Compare(strings.ToLower(v), value)
		if ge && c >= 0 || !ge && c <= 0 {
			return true
		}
	}
	return false
}

func subAttr(e *Entry, attr string, subs []substr) bool {
	for _, v := range e.Attrs[strings.ToLower(attr)] {
		if matchSub(strings.ToLower(v), subs) {
			return true
		}
	}
	return false
}

func matchSub(val string, subs []substr) bool {
	rest := val
	for _, s := range subs {
		part := strings.ToLower(s.val)
		switch s.kind {
		case subInitial:
			if !strings.HasPrefix(rest, part) {
				return false
			}
			rest = rest[len(part):]
		case subFinal:
			return strings.HasSuffix(rest, part)
		default:
			i := strings.Index(rest, part)
			if i < 0 {
				return false
			}
			rest = rest[i+len(part):]
		}
	}
	return true
}

func (d *Directory) inChain(e *Entry, attr, value string) bool {
	attr = strings.ToLower(attr)
	if attr != "member" && attr != "memberof" {
		return false
	}
	want := normalizeDN(value)
	if want == "" {
		return false
	}
	seen := map[string]bool{}
	var walk func(dn string) bool
	walk = func(dn string) bool {
		key := normalizeDN(dn)
		if key == "" || seen[key] {
			return false
		}
		seen[key] = true
		cur := d.byDN[key]
		if cur == nil {
			return false
		}
		next := cur.DirectMembers
		if attr == "memberof" {
			next = cur.DirectMemberOf
		}
		for _, n := range next {
			if normalizeDN(n) == want {
				return true
			}
			if walk(n) {
				return true
			}
		}
		return false
	}
	return walk(e.DN)
}

// scope: 0 base, 1 one level, 2 subtree.
func (d *Directory) candidates(base string, scope int) []*Entry {
	if normalizeDN(base) == "" && scope == 0 {
		return []*Entry{rootDSE(d)}
	}
	var out []*Entry
	for _, e := range d.entries {
		switch scope {
		case 0:
			if normalizeDN(e.DN) == normalizeDN(base) {
				out = append(out, e)
			}
		case 1:
			if normalizeDN(dnParent(e.DN)) == normalizeDN(base) {
				out = append(out, e)
			}
		default:
			if dnUnder(e.DN, base) {
				out = append(out, e)
			}
		}
	}
	return out
}

func (d *Directory) hasBase(base string) bool {
	if strings.TrimSpace(base) == "" {
		return true
	}
	_, ok := d.byDN[normalizeDN(base)]
	return ok
}

func rootDSE(d *Directory) *Entry {
	return &Entry{
		DN: "",
		Attrs: map[string][]string{
			"objectclass":          {"top", "OpenLDAProotDSE"},
			"namingcontexts":       {d.BaseDN},
			"supportedldapversion": {"3"},
			"vendorname":           {"factum2"},
			"subschemasubentry":    {"cn=subschema"},
		},
		operational: map[string]bool{
			"namingcontexts":       true,
			"supportedldapversion": true,
			"vendorname":           true,
			"subschemasubentry":    true,
		},
	}
}

// selected attributes. Empty or "*" is every user attribute. "+" adds
// operational attributes. memberOf is operational, so clients that want
// groups ask for it by name (the RADIUS worker does).
func project(e *Entry, names []string, typesOnly bool) [][2]string {
	wantAll := len(names) == 0
	wantOp := false
	explicit := map[string]bool{}
	for _, n := range names {
		switch n {
		case "*", "":
			wantAll = true
		case "+":
			wantOp = true
		default:
			explicit[strings.ToLower(n)] = true
		}
	}
	var out [][2]string
	for name, vals := range e.Attrs {
		op := e.operational[name]
		if explicit[name] || (!op && wantAll) || (op && wantOp) {
			if typesOnly {
				out = append(out, [2]string{name, ""})
				continue
			}
			for _, v := range vals {
				out = append(out, [2]string{name, v})
			}
		}
	}
	sortPairs(out)
	return out
}

func sortPairs(s [][2]string) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && (s[j][0] < s[j-1][0] || (s[j][0] == s[j-1][0] && s[j][1] < s[j-1][1])) {
			s[j], s[j-1] = s[j-1], s[j]
			j--
		}
	}
}
