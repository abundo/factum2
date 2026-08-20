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

// Add default domain on names without .
func FormatName(defaultDomain string, name string) string {
	if !strings.Contains(name, ".") {
		return name + "." + defaultDomain
	}
	return name
}

// ShortName strips the trailing ".defaultDomain" suffix from a fully
// qualified name - the inverse of FormatName.
func ShortName(name, defaultDomain string) string {
	if defaultDomain == "" {
		return name
	}
	return strings.TrimSuffix(name, "."+defaultDomain)
}
