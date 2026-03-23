package main

import "strings"

// isDevelopment determines if the current application version is a development version by checking if "dev" is in its name.
func isDevelopment() bool {
	return strings.Contains(strings.ToLower(version), "dev")
}
