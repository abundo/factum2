package util

import (
	"encoding/json"
	"fmt"
	"strings"
)

// var Config ConfigRoot

// ----------------------

type Response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// print structures JSON formatted
func Pprint(data any) {
	fmt.Println(StructToString(data))
}

// Return data as JSON formatted string
func StructToString(data any) string {
	s, _ := json.MarshalIndent(data, "", "  ")
	return string(s)
}

// FormatName appends defaultDomain when name has no '.' (a short hostname).
// Names that are already FQDNs (or IPv4 literals) are returned unchanged.
// An empty defaultDomain is a no-op so callers don't get a trailing dot.
func FormatName(defaultDomain string, name string) string {
	if defaultDomain == "" || name == "" || strings.Contains(name, ".") {
		return name
	}
	return name + "." + defaultDomain
}

// ShortName strips the trailing ".defaultDomain" suffix from a fully
// qualified name - the inverse of FormatName.
func ShortName(name, defaultDomain string) string {
	if defaultDomain == "" {
		return name
	}
	return strings.TrimSuffix(name, "."+defaultDomain)
}
