package drivers

import "regexp"

// IOS and IOS-XE have no driver here. Neighbors on those platforms still
// advertise Te/Gi/Fa, and NetBox stores the running-config CamelCase name.
func init() {
	registerInterfaceNames("ios-xe", iosXEExpandInterfaceName)
	registerInterfaceNames("ios", iosXEExpandInterfaceName)
}

var iosXEInterfaceNames = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`^Te(\d.*)$`), "TenGigabitEthernet$1"},
	{regexp.MustCompile(`^Gi(\d.*)$`), "GigabitEthernet$1"},
	{regexp.MustCompile(`^Fa(\d.*)$`), "FastEthernet$1"},
}

func iosXEExpandInterfaceName(name string) string {
	for _, r := range iosXEInterfaceNames {
		if r.re.MatchString(name) {
			return r.re.ReplaceAllString(name, r.repl)
		}
	}
	return name
}
