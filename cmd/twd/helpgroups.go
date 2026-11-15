package main

import (
	"fmt"
	"io"
	"strings"
)

// `twd help` groups the commands with one short line each (issue #46);
// the full usage is `twd <command> --help`, every command's at once
// `twd help --all`.
//
// A group may name a command that isn't registered (on this branch); it is
// skipped. A registered command that's in no group is listed under
// "Other" (TestHelpGroups fails on it): add it to a group and to
// shortHelp, one per line, in logical position.
var helpGroups = []struct {
	title string
	names []string
}{
	{"Set up", []string{
		"up",
		"down",
		"link",
		"tenant",
		"host",
		"doctor",
	}},
	{"Develop", []string{
		"init",
		"dev",
		"types",
		"agent",
		"mcp",
		"docs",
	}},
	{"Deploy", []string{
		"deploy",
		"deployments",
		"rollback",
		"undeploy",
		"expose",
		"secret",
		"image",
	}},
	{"Data", []string{
		"d1",
		"kv",
		"queues",
		"r2",
	}},
	{"Observe", []string{
		"whoami",
		"status",
		"ls",
		"logs",
		"audit",
		"telemetry",
	}},
	{"Operate", []string{
		"kill",
		"approvals",
		"approve",
		"notify",
		"upgrade",
		"release",
		"version",
		"completion",
	}},
}

// shortHelp is each command's line in `twd help` (≤ 60 characters).
var shortHelp = map[string]string{
	"agent":       "teach coding agents to drive twd (skills)",
	"approvals":   "Workflow approvals waiting for a decision",
	"approve":     "approve or reject a waiting Workflow",
	"audit":       "the hash-chained audit trail",
	"completion":  "shell completion script (bash, zsh, fish)",
	"d1":          "a workload's D1 databases and migrations",
	"deploy":      "bundle, place and deploy an app; wait until ready",
	"deployments": "a workload's deployment history",
	"dev":         "run an app locally, no tailnet",
	"docs":        "search and read twd's docs offline",
	"doctor":      "check this machine can run twd",
	"down":        "stop everything twd up started",
	"expose":      "open or close a workload's public routes",
	"host":        "add, list, remove or set up hosts",
	"image":       "public base images and operator images",
	"init":        "create an app from a template",
	"kill":        "kill an object (--cascade: its sub-agents)",
	"kv":          "a workload's Workers KV namespaces and keys",
	"link":        "use a remote twd-mgmt from this machine",
	"logs":        "request records and console lines; daemon logs",
	"ls":          "list deployments and objects",
	"mcp":         "twd's MCP server for coding agents (stdio)",
	"notify":      "a webhook for approvals waiting for a decision",
	"queues":      "the tenant's queues: create, peek, send",
	"r2":          "a workload's R2 buckets and objects",
	"release":     "twd-mgmt's release catalog",
	"rollback":    "redeploy an earlier deployment's bundle",
	"secret":      "a workload's secrets (env.NAME)",
	"status":      "health of mgmt, hosts, tenants, deployments",
	"telemetry":   "the tenant's OTLP export",
	"tenant":      "serve, list, update or remove tenants",
	"types":       "generate Env types from twd.jsonc and wrangler config",
	"undeploy":    "stop a workload and release its lease",
	"up":          "start twd on this machine",
	"upgrade":     "upgrade twd on this machine or the fleet",
	"version":     "print the version; --check for a newer release",
	"whoami":      "who the CLI acts as, and what it may do",
}

// helpLine is c's line in `twd help`: shortHelp, else the help text up to
// its first ";" or " (".
func helpLine(c command) string {
	if s, ok := shortHelp[c.name]; ok {
		return s
	}
	s := c.help
	for _, sep := range []string{";", " ("} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return s
}

// groupedCommands is `twd help`'s layout: each group's registered commands,
// then "Other" for the commands in no group.
func groupedCommands() (titles []string, groups [][]command) {
	byName := map[string]command{}
	for _, c := range commands {
		if !c.hidden {
			byName[c.name] = c
		}
	}
	placed := map[string]bool{}
	for _, g := range helpGroups {
		var cs []command
		for _, n := range g.names {
			if c, ok := byName[n]; ok && !placed[n] {
				cs = append(cs, c)
				placed[n] = true
			}
		}
		if len(cs) > 0 {
			titles, groups = append(titles, g.title), append(groups, cs)
		}
	}
	var other []command
	for _, c := range commands {
		if !c.hidden && !placed[c.name] {
			other = append(other, c)
		}
	}
	if len(other) > 0 {
		titles, groups = append(titles, "Other"), append(groups, other)
	}
	return titles, groups
}

// usage is `twd help` (and `twd`, `twd --help`).
func usage(w io.Writer) { usageGrouped(w) }

// usageGrouped is `twd help`.
func usageGrouped(w io.Writer) {
	fmt.Fprintln(w, "twd: Workers and Durable Objects with their own tailnet identities")
	fmt.Fprintln(w, "\nusage: twd <command> [flags]   (every command takes --state-dir, default ~/.twd, and --json)")
	titles, groups := groupedCommands()
	for i, t := range titles {
		fmt.Fprintf(w, "\n%s:\n", t)
		for _, c := range groups[i] {
			fmt.Fprintf(w, "  %-11s %s\n", c.name, helpLine(c))
		}
	}
	fmt.Fprintln(w, "\ntemplates (twd init --template): hello | agent | mcp | ai-agent | lock | refund-approval | node-workflow (needs Node.js)")
	fmt.Fprintln(w, "\ntwd <command> --help     a command's usage and flags")
	fmt.Fprintln(w, "twd help --all           every command's usage")
	fmt.Fprintln(w, "twd completion --help    tab completion for bash, zsh and fish")
}
