package main

// twd agent (issue #25, docs/agents.md): teach coding agents to drive twd.
//
//	twd agent setup  [--scope user|project] [--dir .] [--agent claude,codex,...] [--dry-run] [--yes]
//	twd agent remove [--scope user|project] [--dir .] [--agent ...] [--dry-run] [--yes]
//	twd agent ls                  the skills, and which agents are installed here
//	twd agent show <skill>        print one skill (for agents that can't load skills)
//
// setup and remove print every file they would write or delete and ask
// before touching anything (--yes skips the question; without a terminal
// it is required). The skills themselves are in agent/skills.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	twdagent "github.com/rosskukulinski/twd/agent"
)

func cmdAgent(args []string) error {
	sub, args, ok := splitSubcommand(args)
	if !ok {
		return fail("bad_request", "twd agent needs a subcommand: setup, remove, ls or show", "usage: twd "+usageOf("agent"))
	}
	switch sub {
	case "setup":
		return cmdAgentSetup(args, false)
	case "remove":
		return cmdAgentSetup(args, true)
	case "ls":
		return cmdAgentLs(args)
	case "show":
		return cmdAgentShow(args)
	}
	return fail("bad_request", "unknown subcommand agent "+sub, "usage: twd "+usageOf("agent"))
}

// splitSubcommand finds the subcommand after any global flags
// (--state-dir X, --json precede it when given before the command) and
// returns it and the other args.
func splitSubcommand(args []string) (string, []string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a, append(append([]string{}, args[:i]...), args[i+1:]...), true
		}
		if (a == "--state-dir" || a == "-state-dir") && i+1 < len(args) {
			i++
		}
	}
	return "", args, false
}

// agentEnv is this machine: $HOME and $PATH.
func agentEnv() (twdagent.Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return twdagent.Env{}, fail("bad_request", "no home directory: "+err.Error())
	}
	return twdagent.Env{Home: home, LookPath: exec.LookPath}, nil
}

func cmdAgentSetup(args []string, remove bool) error {
	verb := "setup"
	if remove {
		verb = "remove"
	}
	f := newFlags("agent")
	scope := f.String("scope", "user", "user: your home directory (every project) | project: this project's files (--dir)")
	dir := f.String("dir", ".", "the project directory (--scope project)")
	agents := f.String("agent", "", "comma-separated agents: claude, codex, cursor, gemini, opencode (default: setup, the ones installed here; remove, all)")
	dryRun := f.Bool("dry-run", false, "print what would change and write nothing")
	yes := f.Bool("yes", false, "don't ask before writing (needed without a terminal; review --dry-run first)")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fail("bad_request", "twd agent "+verb+" takes no arguments", "usage: twd "+usageOf("agent"))
	}
	env, err := agentEnv()
	if err != nil {
		return err
	}
	opts := twdagent.Options{Version: version, Remove: remove}
	switch *scope {
	case "user":
		opts.Scope = twdagent.ScopeUser
	case "project":
		opts.Scope = twdagent.ScopeProject
		d, err := filepath.Abs(*dir)
		if err != nil {
			return fail("bad_request", err.Error())
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			return fail("bad_request", "--dir "+*dir+" is not a directory")
		}
		opts.Dir = d
	default:
		return fail("bad_request", "--scope must be user or project")
	}
	det := twdagent.Detect(env)
	switch {
	case *agents != "":
		for _, id := range strings.Split(*agents, ",") {
			a, ok := twdagent.AgentByID(strings.TrimSpace(id))
			if !ok {
				return fail("bad_request", "unknown agent "+id, "known: claude, codex, cursor, gemini, opencode")
			}
			opts.Agents = append(opts.Agents, a)
		}
	case remove:
		opts.Agents = twdagent.Agents
	default:
		for _, d := range det {
			if d.Detected {
				opts.Agents = append(opts.Agents, d.Agent)
			}
		}
	}
	if len(opts.Agents) == 0 {
		return fail("not_installed", "no coding agent found on this machine (looked for "+agentDirsList()+")").
			withFix("twd agent setup --agent claude,codex   (name the agents to set up)")
	}
	plan, err := twdagent.MakePlan(env, opts)
	if err != nil {
		return fail("bad_request", err.Error())
	}
	res := agentSetupResult{Plan: plan, DryRun: *dryRun, Agents: det}
	show := func() { printAgentPlan(verb, plan, det, env.Home) }
	if *dryRun || plan.Pending() == 0 {
		emit(f, res, func() {
			show()
			if *dryRun {
				fmt.Println("dry run: nothing written")
			} else {
				fmt.Println("nothing to do: everything is up to date")
			}
		})
		return nil
	}
	if !*yes {
		if *f.json || !isTerminal() {
			return fail("confirmation_required", fmt.Sprintf("twd agent %s would change %d files; it asks first", verb, plan.Pending())).
				withFix("twd agent "+verb+" --dry-run   (shows what would change)", "then run it in a terminal, or add --yes")
		}
		show()
		fmt.Printf("%s these %d files? [y/N] ", map[bool]string{false: "Write", true: "Change"}[remove], plan.Pending())
		ln, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(ln)); a != "y" && a != "yes" {
			return fail("cancelled", "nothing was written")
		}
	}
	if err := twdagent.Apply(plan); err != nil {
		return fail("internal", "twd agent "+verb+": "+err.Error())
	}
	res.Applied = true
	emit(f, res, func() {
		if *yes {
			show()
		}
		fmt.Printf("✓ %d files changed\n", plan.Pending())
	})
	return nil
}

type agentSetupResult struct {
	*twdagent.Plan
	DryRun  bool                 `json:"dryRun"`
	Applied bool                 `json:"applied"`
	Agents  []twdagent.Detection `json:"agents"`
}

func agentDirsList() string {
	var out []string
	for _, a := range twdagent.Agents {
		for _, d := range a.Dirs {
			out = append(out, "~/"+d)
		}
	}
	return strings.Join(out, ", ")
}

// tildePath shows p relative to home (~/…) or, for a project, as is.
func tildePath(p, home string) string {
	if r, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok && home != "" {
		return "~/" + filepath.ToSlash(r)
	}
	return p
}

func printAgentPlan(verb string, p *twdagent.Plan, det []twdagent.Detection, home string) {
	fmt.Printf("twd agent %s · scope %s · twd skills %s (%d skills)\n", verb, p.Scope, p.Version, len(twdagent.Skills()))
	for _, d := range det {
		if d.Detected {
			fmt.Printf("  found     %-12s %s\n", d.Name, strings.Join(d.Found, ", "))
		}
	}
	root := p.Root
	if p.Scope == twdagent.ScopeUser {
		root = "~"
	}
	fmt.Printf("  in        %s\n", root)
	unchanged := 0
	for _, c := range p.Changes {
		if c.Action == twdagent.Unchanged {
			unchanged++
			continue
		}
		where := tildePath(c.Path, home)
		if p.Scope == twdagent.ScopeProject {
			if r, err := filepath.Rel(p.Root, c.Path); err == nil {
				where = "./" + filepath.ToSlash(r)
			}
		}
		if c.Kind == "block" {
			where += " (twd block)"
		}
		note := ""
		if c.Note != "" {
			note = " · " + c.Note
		}
		fmt.Printf("  %-9s %-52s %s%s\n", c.Action, where, strings.Join(c.Agents, ", "), note)
	}
	if unchanged > 0 {
		fmt.Printf("  %d files already up to date\n", unchanged)
	}
	for _, s := range p.Skipped {
		fmt.Printf("  skipped   %s\n", s)
	}
}

func cmdAgentLs(args []string) error {
	f := newFlags("agent")
	if _, err := f.parse(args); err != nil {
		return err
	}
	env, err := agentEnv()
	if err != nil {
		return err
	}
	skills := twdagent.Skills()
	for i := range skills {
		skills[i].Body = ""
	}
	det := twdagent.Detect(env)
	emit(f, map[string]any{"version": version, "skills": skills, "agents": det}, func() {
		fmt.Printf("twd skills %s\n", version)
		for _, s := range skills {
			fmt.Printf("  %-14s %s\n", s.Name, s.Description)
		}
		fmt.Println("coding agents on this machine")
		for _, d := range det {
			st := "not found"
			if d.Detected {
				st = strings.Join(d.Found, ", ")
			}
			fmt.Printf("  %-12s %s\n", d.Name, st)
		}
		fmt.Println("install them: twd agent setup --dry-run, then twd agent setup")
	})
	return nil
}

func cmdAgentShow(args []string) error {
	f := newFlags("agent")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail("bad_request", "twd agent show needs one skill name", "usage: twd "+usageOf("agent"))
	}
	s, ok := twdagent.Find(pos[0])
	if !ok {
		var names []string
		for _, s := range twdagent.Skills() {
			names = append(names, s.Name)
		}
		return fail("not_found", "no skill "+pos[0], "skills: "+strings.Join(names, ", "))
	}
	emit(f, s, func() { fmt.Print(s.Body) })
	return nil
}
