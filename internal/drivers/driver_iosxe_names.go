package drivers

import "regexp"

// IOS and IOS-XE have no driver here. Neighbors on those platforms still
// advertise Te/Gi/Fa, and NetBox stores the running-config CamelCase name.
func init() {
	registerInterfaceNames("ios-xe", iosXEExpandInterfaceName)
	registerInterfaceNames("ios", iosXEExpandInterfaceName)
}

// The short prefix is matched in either case (gi5 and Gi5). The replacement
// is the running-config CamelCase form. A name that is already long
// (GigabitEthernet5) does not match, because a digit has to follow the prefix.
var iosXEInterfaceNames = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)^Te(\d.*)$`), "TenGigabitEthernet$1"},
	{regexp.MustCompile(`(?i)^Gi(\d.*)$`), "GigabitEthernet$1"},
	{regexp.MustCompile(`(?i)^Fa(\d.*)$`), "FastEthernet$1"},
}

func iosXEExpandInterfaceName(name string) string {
	for _, r := range iosXEInterfaceNames {
		if r.re.MatchString(name) {
			return r.re.ReplaceAllString(name, r.repl)
		}
	}
	return name
}
