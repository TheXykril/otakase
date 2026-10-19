package main

import (
	_ "embed"
	"regexp"
)

//go:embed config.yaml
var addonConfig string

//go:embed CHANGELOG.md
var botChangelog string

var addonVersionRe = regexp.MustCompile(`(?m)^version:\s*"?([^"\s]+)"?`)

// botVersion is the add-on version from config.yaml.
func botVersion() string {
	if m := addonVersionRe.FindStringSubmatch(addonConfig); m != nil {
		return m[1]
	}
	return "dev"
}
