package radius

import (
	"fmt"
	"net"
	"strings"
)

// ValidateListen accepts an empty address (the worker uses :1812) or a
// host:port string such as ":1812" or "10.0.0.5:1812".
func ValidateListen(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		return fmt.Errorf("radius listen must be host:port")
	}
	return nil
}
