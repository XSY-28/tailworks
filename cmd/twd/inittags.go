package main

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/rosskukulinski/twd/mgmt"
)

var camelRE = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// classLabel turns a DO class name into a tag fragment: SupportAgent ->
// support-agent.
func classLabel(class string) string {
	return workloadLabel(camelRE.ReplaceAllString(class, "${1}-${2}"))
}

// appTags renames a template's tags after the new app, so two apps made
// from one template don't share tags (and with them every grant):
// tailnet.tags become tag:twd-wl-<workload>, and each class's tags
// tag:twd-<workload>-<class>. Only twd's own template tags ("tag:twd-…")
// are renamed; it returns the new twd.jsonc and the old -> new map.
func appTags(raw []byte, wl string) ([]byte, map[string]string, error) {
	cfg, err := mgmt.ParseTwdConfig(raw)
	if err != nil {
		return nil, nil, err
	}
	rename := map[string]string{}
	add := func(old, nu string) {
		if strings.HasPrefix(old, "tag:twd-") && old != nu {
			rename[old] = nu
		}
	}
	for _, t := range cfg.Tailnet.Tags {
		add(t, "tag:twd-wl-"+wl)
	}
	classes := make([]string, 0, len(cfg.Tailnet.Objects))
	for c := range cfg.Tailnet.Objects {
		classes = append(classes, c)
	}
	slices.Sort(classes)
	for _, c := range classes {
		for _, t := range cfg.Tailnet.Objects[c].Tags {
			if _, done := rename[t]; !done {
				add(t, "tag:twd-"+wl+"-"+classLabel(c))
			}
		}
	}
	for old, nu := range rename {
		raw = bytes.ReplaceAll(raw, []byte(`"`+old+`"`), []byte(`"`+nu+`"`))
	}
	return raw, rename, nil
}

// initTags lists the tags of the app in dir, for twd init's summary.
func initTags(dir string) []string {
	a, err := loadApp(dir, "")
	if err != nil {
		return nil
	}
	return a.srcTags()
}
