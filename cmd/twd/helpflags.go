package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Per-command flag help. A command declares its flags only when it runs,
// so `twd <cmd> --help` runs it in help mode: flags.parse returns errHelp
// at once (every command parses before doing anything) and keeps the
// FlagSet here for commandFlags to print.
var (
	helpMode    bool
	helpFlagSet *flag.FlagSet
)

// captureFlags runs c up to its flag parsing and returns its FlagSet (nil
// if it has none or a subcommand is missing).
func captureFlags(c command, args []string) *flag.FlagSet {
	helpMode, helpFlagSet = true, nil
	defer func() { helpMode = false }()
	_ = c.run(args)
	return helpFlagSet
}

// printFlags lists fs's flags (--name <type>  description (default …)),
// without the global --state-dir and --json.
func printFlags(w io.Writer, fs *flag.FlagSet) {
	type row struct{ name, desc string }
	var rows []row
	width := 0
	fs.VisitAll(func(fl *flag.Flag) {
		if fl.Name == "state-dir" || fl.Name == "json" {
			return
		}
		// UnquoteUsage would take a `backticked` word in the description
		// as the value's name (even for a bool): name the type from a copy
		// without backticks, and print the description as written.
		typ, _ := flag.UnquoteUsage(&flag.Flag{Value: fl.Value, Usage: strings.ReplaceAll(fl.Usage, "`", "")})
		desc := fl.Usage
		name := "--" + fl.Name
		if typ != "" {
			name += " <" + typ + ">"
		}
		if d := fl.DefValue; d != "" && d != "false" && d != "0" && d != "0s" && d != "[]" && d != "map[]" {
			desc += fmt.Sprintf(" (default %s)", d)
		}
		rows = append(rows, row{name, desc})
		width = max(width, len(name))
	})
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\nflags:")
	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, r.name, strings.ReplaceAll(r.desc, "\n", "\n  "+strings.Repeat(" ", width+2)))
	}
}
