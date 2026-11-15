package main

import (
	"errors"
	"flag"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// twd completion bash|zsh|fish (issue #46): a completion script built from
// the commands table. Subcommands come from each command's usage line
// ("kv key put <KEY> … | kv key get <KEY>", "expose on|off"), flags from
// the flag set each command (and subcommand) hands back for --help, plus
// the --flags its usage line names. Nothing is hand-maintained, so a new
// command or flag shows up in the next generated script.
//
//	eval "$(twd completion bash)"                                    # bash, this shell
//	twd completion zsh > "${fpath[1]}/_twd"                          # zsh, then restart it
//	twd completion fish > ~/.config/fish/completions/twd.fish        # fish

var completionShells = []string{"bash", "zsh", "fish"}

func cmdCompletion(args []string) error {
	f := newFlags("completion")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail("bad_request", "twd completion needs a shell: bash, zsh or fish", "usage: twd "+usageOf("completion"))
	}
	t := buildCompletionTree()
	var script string
	switch pos[0] {
	case "bash":
		script = t.bash()
	case "zsh":
		script = t.zsh()
	case "fish":
		script = t.fish()
	default:
		return fail("bad_request", fmt.Sprintf("no completion for shell %q (bash, zsh or fish)", pos[0]), "usage: twd "+usageOf("completion"))
	}
	emit(f, map[string]string{"shell": pos[0], "script": script}, func() { fmt.Print(script) })
	return nil
}

// compNode is one command path ("", "kv", "kv key", "kv key put").
type compNode struct {
	subs  []string        // next words, in order
	flags map[string]bool // --long and -s spellings
}

// compTree is every command path, and what the root's words mean.
type compTree struct {
	nodes    map[string]*compNode
	desc     map[string]string // root command → its `twd help` line
	valFlags map[string]bool   // flags that take a value (never a bool flag anywhere)
}

var (
	compLiteral = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	compFlag    = regexp.MustCompile(`^\[?(--?[a-zA-Z][a-zA-Z0-9-]*)`)
	compAltSep  = regexp.MustCompile(`\s+\|\s+`)
)

func (t *compTree) node(path string) *compNode {
	n := t.nodes[path]
	if n == nil {
		n = &compNode{flags: map[string]bool{}}
		t.nodes[path] = n
	}
	return n
}

func (t *compTree) addSub(path, word string) {
	n := t.node(path)
	for _, s := range n.subs {
		if s == word {
			return
		}
	}
	n.subs = append(n.subs, word)
	t.node(joinPath(path, word))
}

func joinPath(path, word string) string {
	if path == "" {
		return word
	}
	return path + " " + word
}

// flagSpelling is how a flag is typed: --name, or -n for one letter.
func flagSpelling(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// buildCompletionTree walks the commands table.
func buildCompletionTree() *compTree {
	t := &compTree{nodes: map[string]*compNode{}, desc: map[string]string{}, valFlags: map[string]bool{}}
	boolFlags := map[string]bool{}
	root := t.node("")
	for _, fl := range []string{"--help", "--json", "--state-dir", "--version"} {
		root.flags[fl] = true
	}
	t.valFlags["--state-dir"] = true
	for _, c := range commands {
		if c.hidden {
			continue
		}
		t.addSub("", c.name)
		t.desc[c.name] = helpLine(c)
		// Subcommands and the --flags of each alternative of the usage line.
		for _, alt := range compAltSep.Split(c.usage, -1) {
			w := strings.Fields(alt)
			if len(w) == 0 || w[0] != c.name {
				continue
			}
			path := c.name
			for _, tok := range w[1:] {
				if compLiteral.MatchString(tok) {
					t.addSub(path, tok)
					path = joinPath(path, tok)
					continue
				}
				if choices := strings.Split(tok, "|"); len(choices) > 1 && allLiteral(choices) {
					for _, ch := range choices {
						t.addSub(path, ch)
					}
				}
				break
			}
			for _, tok := range w[1:] {
				if m := compFlag.FindStringSubmatch(tok); m != nil {
					t.node(path).flags["--"+strings.TrimLeft(m[1], "-")] = true
				}
			}
		}
		// The flag sets: `twd <path> --help` returns it (helpRequest).
		var paths []string
		for p := range t.nodes {
			if p == c.name || strings.HasPrefix(p, c.name+" ") {
				paths = append(paths, p)
			}
		}
		sort.Strings(paths)
		for _, p := range paths {
			fs := probeFlags(c, strings.Fields(p)[1:])
			if fs == nil {
				continue
			}
			n := t.node(p)
			fs.VisitAll(func(fl *flag.Flag) {
				sp := flagSpelling(fl.Name)
				n.flags[sp] = true
				if bf, ok := fl.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
					boolFlags[sp] = true
				} else {
					t.valFlags[sp] = true
				}
			})
			n.flags["--help"] = true
		}
	}
	// A usage line's single-letter flag (-n 50) is spelled --n above: keep
	// the flag set's spelling.
	for _, n := range t.nodes {
		for fl := range n.flags {
			if strings.HasPrefix(fl, "--") && len(fl) == 3 {
				delete(n.flags, fl)
				n.flags[fl[1:]] = true
			}
		}
	}
	for fl := range boolFlags {
		delete(t.valFlags, fl)
	}
	return t
}

func allLiteral(ws []string) bool {
	for _, w := range ws {
		if !compLiteral.MatchString(w) {
			return false
		}
	}
	return true
}

// probeFlags runs `twd <c> <sub…> --help` in-process: every command builds
// its flag set and parses before doing anything else, so this returns the
// flag set and has no effect. nil when the command answers with a usage
// error instead (host, tenant without a subcommand).
func probeFlags(c command, sub []string) (fs *flag.FlagSet) {
	defer func() {
		if recover() != nil {
			fs = nil
		}
	}()
	saved := jsonOut
	defer func() { jsonOut = saved }()
	var hr *helpRequest
	if err := c.run(append(append([]string{}, sub...), "--help")); errors.As(err, &hr) && hr.f != nil {
		return hr.f.FlagSet
	}
	return nil
}

// paths are the tree's command paths, sorted.
func (t *compTree) paths() []string {
	out := make([]string, 0, len(t.nodes))
	for p := range t.nodes {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (t *compTree) flagList(path string) []string {
	n := t.nodes[path]
	out := make([]string, 0, len(n.flags))
	for fl := range n.flags {
		out = append(out, fl)
	}
	sort.Strings(out)
	return out
}

func (t *compTree) valFlagList() []string {
	out := make([]string, 0, len(t.valFlags))
	for fl := range t.valFlags {
		out = append(out, fl)
	}
	sort.Strings(out)
	return out
}

// sq quotes s for sh, zsh and fish ('…', with ' escaped).
func sq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fishQ quotes s for fish, whose single quotes escape ' as \'.
func fishQ(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

// shCases is the body of a `case "$1" in … esac` for bash and zsh: each
// path's words, space-separated.
func (t *compTree) shCases(words func(string) []string) string {
	var b strings.Builder
	for _, p := range t.paths() {
		ws := words(p)
		if len(ws) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\t%s) echo %s ;;\n", sq(p), sq(strings.Join(ws, " ")))
	}
	return b.String()
}

func (t *compTree) subsOf(p string) []string { return t.nodes[p].subs }

const compHeader = "# twd completion for %s, generated by `twd completion %s` from twd %s's commands.\n# %s\n"

func (t *compTree) bash() string {
	var b strings.Builder
	fmt.Fprintf(&b, compHeader, "bash", "bash", version, `Load it: eval "$(twd completion bash)"  (in ~/.bashrc), or save it in ~/.local/share/bash-completion/completions/twd`)
	fmt.Fprintf(&b, "_twd_subs() {\n\tcase \"$1\" in\n%s\tesac\n}\n", t.shCases(t.subsOf))
	fmt.Fprintf(&b, "_twd_flags() {\n\tcase \"$1\" in\n%s\tesac\n}\n", t.shCases(t.flagList))
	fmt.Fprintf(&b, "_twd_valflags=%s\n", sq(" "+strings.Join(t.valFlagList(), " ")+" "))
	b.WriteString(`_twd() {
	local cur="${COMP_WORDS[COMP_CWORD]}" p="" done="" skip="" w i
	for ((i = 1; i < COMP_CWORD; i++)); do
		w="${COMP_WORDS[i]}"
		if [[ -n "$skip" ]]; then skip=""; continue; fi
		if [[ "$w" == -* ]]; then
			[[ "$_twd_valflags" == *" $w "* ]] && skip=1
			continue
		fi
		[[ -n "$done" ]] && continue
		if [[ " $(_twd_subs "$p") " == *" $w "* ]]; then p="${p:+$p }$w"; else done=1; fi
	done
	if [[ "$cur" == -* ]]; then
		COMPREPLY=($(compgen -W "$(_twd_flags "$p")" -- "$cur"))
	elif [[ -z "$done" && -z "$skip" ]]; then
		COMPREPLY=($(compgen -W "$(_twd_subs "$p")" -- "$cur"))
	fi
}
complete -o default -F _twd twd
`)
	return b.String()
}

func (t *compTree) zsh() string {
	var b strings.Builder
	b.WriteString("#compdef twd\n")
	fmt.Fprintf(&b, compHeader, "zsh", "zsh", version, `Load it: twd completion zsh > "${fpath[1]}/_twd" and restart zsh, or source <(twd completion zsh) after compinit`)
	fmt.Fprintf(&b, "_twd_subs() {\n\tcase \"$1\" in\n%s\tesac\n}\n", t.shCases(t.subsOf))
	fmt.Fprintf(&b, "_twd_flags() {\n\tcase \"$1\" in\n%s\tesac\n}\n", t.shCases(t.flagList))
	b.WriteString("_twd_commands=(\n")
	for _, c := range t.nodes[""].subs {
		fmt.Fprintf(&b, "\t%s\n", sq(c+":"+strings.ReplaceAll(t.desc[c], ":", `\:`)))
	}
	b.WriteString(")\n")
	fmt.Fprintf(&b, "_twd_valflags=%s\n", sq(" "+strings.Join(t.valFlagList(), " ")+" "))
	b.WriteString(`_twd() {
	local p="" done="" skip="" w i
	local -a subs
	for ((i = 2; i < CURRENT; i++)); do
		w="${words[i]}"
		if [[ -n "$skip" ]]; then skip=""; continue; fi
		if [[ "$w" == -* ]]; then
			[[ "$_twd_valflags" == *" $w "* ]] && skip=1
			continue
		fi
		[[ -n "$done" ]] && continue
		subs=(${=$(_twd_subs "$p")})
		if (( ${subs[(Ie)$w]} )); then p="${p:+$p }$w"; else done=1; fi
	done
	if [[ "$PREFIX" == -* ]]; then
		compadd -- ${=$(_twd_flags "$p")}
	elif [[ -n "$skip" ]]; then
		_files
	elif [[ -z "$done" && -z "$p" ]]; then
		_describe 'twd command' _twd_commands
	elif [[ -z "$done" && -n "$(_twd_subs "$p")" ]]; then
		compadd -- ${=$(_twd_subs "$p")}
	else
		_files
	fi
}
if [[ "${funcstack[1]}" == "_twd" ]]; then
	_twd "$@"
else
	compdef _twd twd
fi
`)
	return b.String()
}

func (t *compTree) fish() string {
	var b strings.Builder
	fmt.Fprintf(&b, compHeader, "fish", "fish", version, "Load it: twd completion fish > ~/.config/fish/completions/twd.fish")
	fishSwitch := func(name string, words func(string) []string) {
		fmt.Fprintf(&b, "function %s\n\tswitch \"$argv[1]\"\n", name)
		for _, p := range t.paths() {
			ws := words(p)
			if len(ws) == 0 {
				continue
			}
			qs := make([]string, len(ws))
			for i, w := range ws {
				qs[i] = fishQ(w)
			}
			fmt.Fprintf(&b, "\t\tcase %s\n\t\t\tprintf '%%s\\n' %s\n", fishQ(p), strings.Join(qs, " "))
		}
		b.WriteString("\tend\nend\n")
	}
	fishSwitch("__twd_subs", t.subsOf)
	fishSwitch("__twd_flags", t.flagList)
	vq := make([]string, 0, len(t.valFlags))
	for _, fl := range t.valFlagList() {
		vq = append(vq, fishQ(fl))
	}
	fmt.Fprintf(&b, "set -g __twd_valflags %s\n", strings.Join(vq, " "))
	b.WriteString(`# __twd_path is the command path typed so far ("kv key"), prefixed with
# "=" once a positional argument ends the subcommands.
function __twd_path
	set -l toks (commandline -opc)
	set -e toks[1]
	set -l p ''
	set -l skip 0
	for t in $toks
		if test $skip = 1
			set skip 0
			continue
		end
		if string match -q -- '-*' $t
			contains -- $t $__twd_valflags; and set skip 1
			continue
		end
		if contains -- $t (__twd_subs "$p")
			set p (string trim -- "$p $t")
		else
			echo "=$p"
			return
		end
	end
	if test $skip = 1
		echo "=$p"
		return
	end
	echo $p
end
function __twd_at_root
	set -l p (__twd_path)
	test -z "$p"
end
function __twd_has_subs
	set -l p (__twd_path)
	string match -q -- '=*' "$p"; and return 1
	test -n "$p"; and test (count (__twd_subs "$p")) -gt 0
end
function __twd_sub_words
	__twd_subs (__twd_path)
end
function __twd_flag_words
	__twd_flags (string replace -r '^=' '' -- (__twd_path))
end
complete -c twd -n 'string match -q -- "-*" (commandline -ct)' -f -a '(__twd_flag_words)'
complete -c twd -n '__twd_has_subs' -f -a '(__twd_sub_words)'
`)
	for _, c := range t.nodes[""].subs {
		fmt.Fprintf(&b, "complete -c twd -n __twd_at_root -f -a %s -d %s\n", fishQ(c), fishQ(t.desc[c]))
	}
	return b.String()
}
