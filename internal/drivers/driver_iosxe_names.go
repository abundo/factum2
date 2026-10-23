package drivers

import "regexp"

// IOS-XE has no driver here. Neighbors on that platform still advertise
// Te/Gi/Fa, and NetBox stores the running-config CamelCase name.
func init() {
	registerInterfaceNames("ios-xe", iosXEExpandInterfaceName)
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
