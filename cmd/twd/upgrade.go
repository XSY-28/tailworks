package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/rosskukulinski/twd/internal/catalog"
	twdversion "github.com/rosskukulinski/twd/internal/version"
	"github.com/rosskukulinski/twd/mgmt"
)

// twd upgrade (ADR-041 §8).
//
//   - On a `twd up` machine (homelab): download (or take --from) a release,
//     verify it, show the plan from its catalog, back up mgmt's store with
//     custody, stop, install (install.sh keeps the previous release as
//     .prev), start, health-check, and roll back automatically if the
//     health check fails and no contract migration ran.
//   - Against a linked twd-mgmt (`twd link`), or with --fleet: ask mgmt for a
//     plan and start an upgrade run that mgmt drives (pause, resume, abort,
//     status as subcommands).

const upgradeUsage = "upgrade [<version>] [--from <tarball|dir>] [--plan] [--yes] [--fleet] [--hosts h1,h2] [--canary h] [--soak 5m] [--settle 5m] [--no-wait] | upgrade status|pause|resume|abort [<run-id>]"

func cmdUpgrade(args []string) error {
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		switch a {
		case "status", "pause", "resume", "abort":
			return cmdUpgradeRun(a, append(append([]string{}, args[:i]...), args[i+1:]...))
		}
		break
	}
	f := newFlags("upgrade")
	from := f.String("from", "", "install this release tarball (twd-<v>-<os>-<arch>.tar.gz, with SHA256SUMS next to it) or extracted release directory instead of downloading")
	planOnly := f.Bool("plan", false, "print the plan and change nothing")
	yes := f.Bool("yes", false, "don't ask for confirmation")
	fleet := f.Bool("fleet", false, "upgrade the fleet through twd-mgmt (an upgrade run) even on a twd up machine")
	hosts := f.String("hosts", "", "fleet: only these hosts (comma-separated)")
	canary := f.String("canary", "", "fleet: the canary host (default: the first)")
	soak := f.Duration("soak", mgmt.DefaultUpgradeSoak, "fleet: watch the canary this long before the other hosts")
	settle := f.Duration("settle", mgmt.DefaultUpgradeSettle, "fleet: how long a host may take to come back and settle")
	noWait := f.Bool("no-wait", false, "fleet: start the run and return (twd upgrade status follows it)")
	health := f.Duration("health-timeout", 3*time.Minute, "homelab: how long the upgraded twd may take to report healthy before it is rolled back")
	noRollback := f.Bool("no-rollback", false, "homelab: keep the new release even if the health check fails")
	prefixFlag := f.String("prefix", "", "homelab: the install.sh prefix to upgrade (default: the one that owns --state-dir)")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fail("bad_request", "twd upgrade takes at most one version", "usage: twd "+upgradeUsage)
	}
	want := ""
	if len(pos) == 1 {
		v, err := twdversion.Parse(pos[0])
		if err != nil {
			return fail("bad_request", err.Error())
		}
		want = v.String()
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	if *fleet || s.remote() {
		req := mgmt.UpgradeRequest{To: want, Hosts: splitComma(*hosts), Canary: *canary}
		so, se := int(soak.Seconds()), int(settle.Seconds())
		req.SoakSeconds, req.SettleSeconds = &so, &se
		return fleetUpgrade(f, s, req, *planOnly, *yes, *noWait)
	}
	return homelabUpgrade(f, s, want, *from, *prefixFlag, *planOnly, *yes, *health, *noRollback)
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func askYes(f *flags, question string, yes bool, what string) error {
	if yes {
		return nil
	}
	if *f.json || !isTerminal() {
		return fail("confirmation_required", what+"; it asks first").
			withFix("twd upgrade --plan   (shows what it would do)", "then run it in a terminal, or add --yes")
	}
	fmt.Printf("%s [y/N] ", question)
	ln, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(ln)); a != "y" && a != "yes" {
		return fail("cancelled", "nothing was changed")
	}
	return nil
}

// ---- fleet ----

func fleetUpgrade(f *flags, s *state, req mgmt.UpgradeRequest, planOnly, yes, noWait bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := s.mgmt()
	hdr := map[string]string{"Twd-Actor": actor()}
	if req.To == "" {
		var cat struct {
			Current string              `json:"current"`
			Items   []mgmt.ReleaseEntry `json:"items"`
		}
		if err := c.DoHeaders(ctx, "GET", "/v1/releases", nil, &cat, hdr); err != nil {
			return err
		}
		if len(cat.Items) == 0 {
			return fail("bad_request", "twd-mgmt's release catalog is empty").withFix("twd release import twd-<version>-<os>-<arch>.tar.gz")
		}
		req.To = cat.Items[len(cat.Items)-1].Version
	}
	req.DryRun = true
	var plan mgmt.UpgradePlan
	if err := c.DoHeaders(ctx, "POST", "/v1/upgrades", req, &plan, hdr); err != nil {
		return err
	}
	if planOnly {
		emit(f, plan, func() { printFleetPlan(plan) })
		return nil
	}
	if len(plan.Blockers) > 0 {
		if !*f.json {
			printFleetPlan(plan)
		}
		return fail("upgrade_blocked", "the upgrade can't start: "+strings.Join(plan.Blockers, "; ")).withFix("twd upgrade --plan " + req.To)
	}
	if !yes && !*f.json && isTerminal() {
		printFleetPlan(plan)
	}
	if err := askYes(f, "Start the upgrade run to "+req.To+"?", yes, "twd upgrade would start a fleet upgrade run to "+req.To); err != nil {
		return err
	}
	req.DryRun = false
	var run mgmt.UpgradeRun
	if err := c.DoHeaders(ctx, "POST", "/v1/upgrades", req, &run, hdr); err != nil {
		return err
	}
	if noWait {
		emit(f, run, func() {
			fmt.Printf("✓ upgrade run %s started (to %s); follow it: twd upgrade status %s\n", run.ID, run.To, run.ID)
		})
		return nil
	}
	return followRun(f, s, run.ID)
}

func printFleetPlan(p mgmt.UpgradePlan) {
	fmt.Printf("Upgrade to twd %s\n", p.To)
	fmt.Printf("  mgmt replicas run: %s\n", strings.Join(orNone(p.From.Mgmt), ", "))
	for _, st := range p.Steps {
		line := "  · " + st.Kind
		if st.Host != "" {
			line += " " + st.Host
		}
		if st.Canary {
			line += " (canary)"
		}
		if st.Method != "" {
			line += " [" + st.Method + "]"
		}
		line += ": " + st.Status
		if st.Detail != "" {
			line += " — " + st.Detail
		}
		fmt.Println(line)
		if st.NeedsOperator != "" {
			fmt.Println("      operator: " + st.NeedsOperator)
		}
	}
	printNotes("Breaking changes", p.Breaking)
	printNotes("Operator actions", p.Actions)
	for _, w := range p.Warnings {
		fmt.Println("  ! " + w)
	}
	for _, b := range p.Blockers {
		fmt.Println("  ✗ " + b)
	}
}

func orNone(xs []string) []string {
	if len(xs) == 0 {
		return []string{"(none)"}
	}
	return xs
}

func printNotes(title string, ns []catalog.Note) {
	if len(ns) == 0 {
		return
	}
	fmt.Println(title + ":")
	for _, n := range ns {
		text := strings.TrimSpace(n.Text)
		if i := strings.IndexByte(text, '\n'); i > 0 {
			text = text[:i] + " …"
		}
		fmt.Printf("  - %s (%s)\n", text, n.Source)
	}
}

func cmdUpgradeRun(action string, args []string) error {
	f := newFlags("upgrade " + action)
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := s.mgmt()
	hdr := map[string]string{"Twd-Actor": actor()}
	id := ""
	if len(pos) > 0 {
		id = pos[0]
	} else {
		var runs struct {
			Items []mgmt.UpgradeRun `json:"items"`
		}
		if err := c.DoHeaders(ctx, "GET", "/v1/upgrades", nil, &runs, hdr); err != nil {
			return err
		}
		if len(runs.Items) == 0 {
			if action == "status" {
				emit(f, map[string]any{"runs": []any{}}, func() { fmt.Println("no upgrade runs") })
				return nil
			}
			return fail("not_found", "no upgrade run")
		}
		id = runs.Items[0].ID // newest
	}
	if action == "status" {
		var run mgmt.UpgradeRun
		if err := c.DoHeaders(ctx, "GET", "/v1/upgrades/"+id, nil, &run, hdr); err != nil {
			return err
		}
		emit(f, run, func() { printRun(run) })
		return nil
	}
	var run mgmt.UpgradeRun
	if err := c.DoHeaders(ctx, "POST", "/v1/upgrades/"+id+"/actions", map[string]string{"action": action}, &run, hdr); err != nil {
		return err
	}
	emit(f, run, func() { fmt.Printf("✓ upgrade run %s: %s\n", run.ID, run.State) })
	return nil
}

func printRun(run mgmt.UpgradeRun) {
	fmt.Printf("upgrade run %s → twd %s: %s", run.ID, run.To, run.State)
	if run.Reason != "" {
		fmt.Printf(" (%s)", run.Reason)
	}
	fmt.Println()
	for i, st := range run.Steps {
		mark := "  "
		if i == run.Step && (run.State == "running" || run.State == "paused") {
			mark = "▸ "
		}
		line := mark + st.Kind
		if st.Host != "" {
			line += " " + st.Host
		}
		line += ": " + st.Status
		if st.Phase != "" && st.Status != "done" {
			line += " (" + st.Phase + ")"
		}
		if st.Detail != "" {
			line += " — " + st.Detail
		}
		fmt.Println(line)
		if st.NeedsOperator != "" {
			fmt.Println("    operator: " + st.NeedsOperator)
		}
	}
}

// followRun polls a run until it stops running.
func followRun(f *flags, s *state, id string) error {
	c := s.mgmt()
	last := ""
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var run mgmt.UpgradeRun
		err := c.DoHeaders(ctx, "GET", "/v1/upgrades/"+id, nil, &run, map[string]string{"Twd-Actor": actor()})
		cancel()
		if err != nil {
			return err
		}
		if !*f.json {
			cur := fmt.Sprintf("%s %d", run.State, run.Step)
			if run.Step < len(run.Steps) {
				st := run.Steps[run.Step]
				cur += " " + st.Host + " " + st.Phase + " " + st.Detail + " " + st.NeedsOperator
				if cur != last {
					fmt.Printf("· %s %s %s %s\n", st.Kind, st.Host, st.Phase, st.Detail)
					if st.NeedsOperator != "" {
						fmt.Println("  operator: " + st.NeedsOperator)
					}
				}
			}
			last = cur
		}
		switch run.State {
		case "running":
			time.Sleep(3 * time.Second)
			continue
		case "done":
			emit(f, run, func() { fmt.Printf("✓ upgrade run %s: every component runs twd %s\n", run.ID, run.To) })
			return nil
		default:
			if !*f.json {
				printRun(run)
			}
			return fail("upgrade_"+run.State, fmt.Sprintf("upgrade run %s is %s: %s", run.ID, run.State, run.Reason)).
				withFix("fix the cause, then: twd upgrade resume "+run.ID, "or: twd upgrade abort "+run.ID)
		}
	}
}

// ---- homelab ----

// localPlan is `twd upgrade --plan` on a twd up machine.
type localPlan struct {
	From               string         `json:"from"`
	To                 string         `json:"to"`
	Source             string         `json:"source"`
	Breaking           []catalog.Note `json:"breaking"`
	Actions            []catalog.Note `json:"actions"`
	ContractMigrations []string       `json:"contractMigrations"`
	Workerd            string         `json:"workerd,omitempty"`
	Steps              []string       `json:"steps"`
	Warnings           []string       `json:"warnings"`
}

type localResult struct {
	Plan       localPlan `json:"plan"`
	Upgraded   bool      `json:"upgraded"`
	Backup     string    `json:"backup,omitempty"`
	RolledBack bool      `json:"rolledBack,omitempty"`
	Health     string    `json:"health,omitempty"`
	// NotActiveBefore are deployments that weren't active before the
	// upgrade: the health check didn't count them (issue #63).
	NotActiveBefore []string `json:"notActiveBefore,omitempty"`
}

func homelabUpgrade(f *flags, s *state, want, from, prefixFlag string, planOnly, yes bool, healthTimeout time.Duration, noRollback bool) error {
	pc, err := resolvePrefix(prefixFlag, installPrefixOf(), statePrefixCandidates(s))
	if err != nil {
		return err
	}
	prefix := pc.Prefix
	if prefix == "" && !planOnly {
		return fail("bad_request", "this twd doesn't run from an install.sh prefix (bin/, share/twd, VERSION), so it can't upgrade itself in place").
			withFix("install a release with scripts/get.sh, or upgrade by hand: tar -xzf twd-<v>-…tar.gz && ./twd-<v>-…/install.sh",
				"or name the install to upgrade: twd upgrade --prefix <dir>")
	}
	if pc.Mismatch != "" && !planOnly {
		return fail("prefix_mismatch", pc.Mismatch+": twd upgrade won't guess which one to replace").
			withFix("twd upgrade --prefix "+prefix+" …   (upgrade the install that runs the state dir)",
				"or run the twd installed there: "+filepath.Join(prefix, "bin", "twd")+" upgrade …")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	stage := filepath.Join(s.Dir, "upgrade")
	os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	rel, source, err := fetchRelease(ctx, want, from, stage)
	if err != nil {
		return err
	}
	meta, err := readReleaseMeta(rel)
	if err != nil {
		return err
	}
	if meta.OS != runtime.GOOS || meta.Arch != runtime.GOARCH {
		return fail("bad_request", fmt.Sprintf("the release is for %s-%s, this machine is %s-%s", meta.OS, meta.Arch, runtime.GOOS, runtime.GOARCH))
	}
	cur := installedVersion(prefix)
	plan := localPlanFor(cur, meta, rel, source, s)
	if pc.Mismatch != "" {
		plan.Warnings = append(plan.Warnings, pc.Mismatch+": the run needs --prefix "+prefix)
	}
	if s.supervisorAlive() {
		plan.Warnings = append(plan.Warnings, notActiveWarnings(snapshotDeployments(s))...)
	}
	res := localResult{Plan: plan}
	if planOnly {
		emit(f, res, func() { printLocalPlan(plan) })
		return nil
	}
	if tv, err := twdversion.Parse(meta.Version); err == nil {
		if cv, err := twdversion.Parse(cur); err == nil && tv.Compare(cv) <= 0 {
			emit(f, res, func() { fmt.Printf("twd %s is installed; the release is %s: nothing to upgrade\n", cur, meta.Version) })
			return nil
		}
	}
	if !*f.json && !yes && isTerminal() {
		printLocalPlan(plan)
	}
	if err := askYes(f, "Upgrade twd "+cur+" → "+meta.Version+"?", yes, "twd upgrade would replace twd "+cur+" with "+meta.Version); err != nil {
		return err
	}

	say := func(format string, a ...any) {
		if !*f.json {
			fmt.Printf(format+"\n", a...)
		}
	}
	// 1. Back up the store with custody (they are only valid together).
	before, _ := mgmt.CheckSchema(ctx, filepath.Join(s.Dir, "mgmt", "mgmt.db"))
	backup, err := backupMgmt(ctx, s, cur)
	if err != nil {
		return fail("upgrade_failed", "backup: "+err.Error())
	}
	res.Backup = backup
	say("✓ backup   %s", backup)

	// 2. Stop, install, start. What was broken before isn't held against
	// the new release by the health check.
	wasService, wasRunning := restartMode(s)
	var prior deploymentsSnapshot
	if wasService || wasRunning {
		prior = snapshotDeployments(s)
		res.NotActiveBefore = wasFailedBefore(prior)
		for _, l := range res.NotActiveBefore {
			say("! %s: the health check won't count it", l)
		}
	}
	if err := stopTwd(s, prefix, wasService, wasRunning); err != nil {
		return err
	}
	inst := exec.CommandContext(ctx, "sh", filepath.Join(rel, "install.sh"), "--prefix", prefix, "--force")
	if out, err := inst.CombinedOutput(); err != nil {
		startTwd(s, prefix, wasService, wasRunning) // the old release is still in place
		return fail("upgrade_failed", "install.sh failed: "+err.Error(), lastLinesOf(string(out), 5)...)
	}
	say("✓ installed twd %s (twd %s kept as %s)", meta.Version, cur, filepath.Join(prefix, "bin.prev"))
	if err := startTwd(s, prefix, wasService, wasRunning); err != nil {
		say("! start failed: %v", err)
	}

	// 3. Health check.
	if wasService || wasRunning {
		err = waitHealthy(s, meta.Version, healthTimeout, prior)
	}
	if err == nil {
		res.Upgraded, res.Health = true, "ok"
		emit(f, res, func() {
			fmt.Printf("✓ twd %s is up and healthy\n", meta.Version)
			for _, l := range res.NotActiveBefore {
				fmt.Println("  ! " + l + " (not checked; `twd deploy` it again)")
			}
			fmt.Println("  installed agent skills are stamped with the old version: re-run `twd agent setup` if you use them")
		})
		return nil
	}
	res.Health = err.Error()
	after, _ := mgmt.CheckSchema(ctx, filepath.Join(s.Dir, "mgmt", "mgmt.db"))
	contracted := after.MinBinary != "" && after.MinBinary != before.MinBinary
	if noRollback || contracted {
		why := "--no-rollback"
		if contracted {
			why = "a contract migration ran (the old twd can't open the store)"
		}
		return fail("upgrade_failed", fmt.Sprintf("twd %s did not become healthy (%v); not rolled back: %s", meta.Version, err, why)).
			withFix("twd logs --component mgmt   and   twd doctor",
				"to go back: twd down; restore "+backup+" into "+filepath.Join(s.Dir, "mgmt")+"; "+filepath.Join(prefix, "install.sh")+" --rollback; twd-mgmt restore-fence -state-dir "+filepath.Join(s.Dir, "mgmt")+"; twd up")
	}
	say("! twd %s did not become healthy (%v): rolling back to %s", meta.Version, err, cur)
	if rerr := rollback(ctx, s, prefix, backup, wasService, wasRunning); rerr != nil {
		return fail("upgrade_failed", fmt.Sprintf("health check failed (%v) and the rollback failed too: %v", err, rerr)).
			withFix("restore by hand: twd down; restore " + backup + "; " + filepath.Join(prefix, "install.sh") + " --rollback; twd-mgmt restore-fence; twd up")
	}
	res.RolledBack = true
	if *f.json {
		emit(f, res, func() {})
	}
	return fail("upgrade_failed", fmt.Sprintf("twd %s did not become healthy (%v); rolled back to %s, store restored from %s", meta.Version, err, cur, backup)).
		withFix("twd logs --component mgmt   (why the new release failed)")
}

// installPrefixOf is the install.sh prefix this twd binary runs from, or
// "". It is not necessarily the install twd upgrade replaces (resolvePrefix).
func installPrefixOf() string {
	if p := filepath.Dir(exeDir()); isInstallPrefix(p) {
		return p
	}
	return ""
}

func installedVersion(prefix string) string {
	if b, err := os.ReadFile(filepath.Join(prefix, "VERSION")); err == nil {
		return strings.TrimSpace(string(b))
	}
	return version
}

type releaseMeta struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Workerd string `json:"workerd"`
}

func readReleaseMeta(dir string) (releaseMeta, error) {
	var m releaseMeta
	b, err := os.ReadFile(filepath.Join(dir, "share", "twd", "release.json"))
	if err != nil {
		return m, fail("bad_request", dir+" is not a twd release (no share/twd/release.json)")
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fail("bad_request", "release.json: "+err.Error())
	}
	return m, nil
}

func localPlanFor(cur string, meta releaseMeta, rel, source string, s *state) localPlan {
	p := localPlan{From: cur, To: meta.Version, Source: source, Breaking: []catalog.Note{}, Actions: []catalog.Note{},
		ContractMigrations: []string{}, Warnings: []string{}}
	es := catalog.Embedded()
	if b, err := os.ReadFile(filepath.Join(rel, "share", "twd", "catalog.json")); err == nil {
		if more, err := catalog.Parse(b); err == nil {
			es = mergeEntries(es, more)
		}
	} else {
		p.Warnings = append(p.Warnings, "the release has no catalog (share/twd/catalog.json): no release notes to show")
	}
	from := cur
	if _, err := twdversion.Parse(from); err != nil {
		from = twdversion.Legacy
	}
	between := catalog.Between(es, from, meta.Version)
	p.Breaking = append(p.Breaking, catalog.Breaking(between)...)
	for _, e := range between {
		p.ContractMigrations = append(p.ContractMigrations, e.ContractMigrations...)
		for _, n := range e.Notes {
			if n.Kind == "breaking" {
				continue
			}
			act := strings.Contains(n.Text, "Operator action:")
			for _, k := range n.Upgrade {
				act = act || slices.Contains([]string{"schema-contract", "workerd", "config-default", "compat-date"}, k)
			}
			if act {
				p.Actions = append(p.Actions, n)
			}
		}
	}
	if meta.Workerd != "" && meta.Workerd != twdversion.WorkerdPin {
		p.Workerd = twdversion.WorkerdPin + " → " + meta.Workerd
	}
	p.Steps = []string{
		"back up " + filepath.Join(s.Dir, "mgmt") + " (store + custody) into " + filepath.Join(s.Dir, "backups"),
		"stop twd",
		"install twd " + meta.Version + " (the current release is kept as bin.prev / share.prev)",
		"start twd and wait until it is healthy",
		"if it isn't: roll back to " + cur + " and restore the store (unless a contract migration ran)",
	}
	if inst, _ := serviceOf(s); !inst && !s.supervisorAlive() {
		p.Steps[3] = "twd is not running: install only"
	}
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/etc/twd/setup.json"); err == nil {
			p.Warnings = append(p.Warnings, "the microVM tier's helper is root-owned: after the upgrade run `sudo "+filepath.Join(filepathDirOr(exeDir()), "twd")+" host setup --microvm` if twd doctor says its protocol is out of range")
		}
	}
	return p
}

func filepathDirOr(d string) string {
	if d == "" {
		return "."
	}
	return d
}

func printLocalPlan(p localPlan) {
	fmt.Printf("Upgrade twd %s → %s (%s)\n", p.From, p.To, p.Source)
	for i, st := range p.Steps {
		fmt.Printf("  %d. %s\n", i+1, st)
	}
	if p.Workerd != "" {
		fmt.Println("  workerd: " + p.Workerd)
	}
	if len(p.ContractMigrations) > 0 {
		fmt.Println("  contract migrations (no automatic rollback after these): " + strings.Join(p.ContractMigrations, ", "))
	}
	printNotes("Breaking changes", p.Breaking)
	printNotes("Operator actions", p.Actions)
	for _, w := range p.Warnings {
		fmt.Println("  ! " + w)
	}
}

// fetchRelease puts the release's extracted directory under stage and
// says where it came from.
func fetchRelease(ctx context.Context, want, from, stage string) (dir, source string, err error) {
	if from != "" {
		st, err := os.Stat(from)
		if err != nil {
			return "", "", fail("bad_request", err.Error())
		}
		if st.IsDir() {
			return from, from, nil
		}
		if sums, err := os.ReadFile(filepath.Join(filepath.Dir(from), "SHA256SUMS")); err == nil {
			if err := checkSum(from, sums); err != nil {
				return "", "", err
			}
		} else {
			return "", "", fail("bad_request", "no SHA256SUMS next to "+from+": twd upgrade verifies a release before installing it").
				withFix("put the release's SHA256SUMS next to the tarball, or extract it and pass the directory")
		}
		d, err := extractRelease(from, stage)
		return d, from, err
	}
	repo := cmpOr(os.Getenv("TWD_REPO"), "rosskukulinski/twd")
	tag := ""
	if want == "" {
		tag, err = latestTag(ctx, repo)
		if err != nil {
			return "", "", err
		}
	} else {
		tag = "v" + want
	}
	v := strings.TrimPrefix(tag, "v")
	name := fmt.Sprintf("twd-%s-%s-%s.tar.gz", v, runtime.GOOS, runtime.GOARCH)
	base := cmpOr(os.Getenv("TWD_RELEASE_BASE"), "https://github.com/"+repo+"/releases/download/"+tag)
	tgz := filepath.Join(stage, name)
	if err := download(ctx, base+"/"+name, tgz); err != nil {
		return "", "", err
	}
	sumsPath := filepath.Join(stage, "SHA256SUMS")
	if err := download(ctx, base+"/SHA256SUMS", sumsPath); err != nil {
		return "", "", err
	}
	sums, _ := os.ReadFile(sumsPath)
	if err := checkSum(tgz, sums); err != nil {
		return "", "", err
	}
	d, err := extractRelease(tgz, stage)
	return d, base + "/" + name, err
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func latestTag(ctx context.Context, repo string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fail("network", "finding the latest release: "+err.Error())
	}
	defer resp.Body.Close()
	var r struct {
		Tag string `json:"tag_name"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&r) != nil || r.Tag == "" {
		return "", fail("not_found", "no published twd release found in "+repo).withFix("twd upgrade <version>   or   twd upgrade --from <tarball>")
	}
	return r.Tag, nil
}

func download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fail("network", "download "+url+": "+err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fail("not_found", "download "+url+": "+resp.Status)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(resp.Body, 2<<30))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func checkSum(path string, sums []byte) error {
	name := filepath.Base(path)
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fs := strings.Fields(line)
		if len(fs) == 2 && strings.TrimPrefix(fs[1], "*") == name {
			want = fs[0]
		}
	}
	if want == "" {
		return fail("bad_request", "SHA256SUMS has no entry for "+name)
	}
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fail("bad_request", fmt.Sprintf("checksum mismatch for %s (got %s, want %s)", name, got, want))
	}
	return nil
}

// extractRelease unpacks a release tarball into stage/x and returns its
// twd-<v>-<os>-<arch> directory.
func extractRelease(tgz, stage string) (string, error) {
	f, err := os.Open(tgz)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return "", fail("bad_request", tgz+": "+err.Error())
	}
	root := filepath.Join(stage, "x")
	tr := tar.NewReader(zr)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		p := filepath.Join(root, hd.Name)
		if !strings.HasPrefix(p, root+string(os.PathSeparator)) {
			return "", fail("bad_request", "the tarball has an entry outside its directory: "+hd.Name)
		}
		switch hd.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(p), 0o755)
			out, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hd.Mode)&0o755|0o600)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(out, io.LimitReader(tr, 1<<30))
			out.Close()
			if err != nil {
				return "", err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hd.Linkname) || strings.Contains(hd.Linkname, "..") {
				return "", fail("bad_request", "the tarball has a symlink outside its directory: "+hd.Name)
			}
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.Symlink(hd.Linkname, p)
		}
	}
	ds, _ := filepath.Glob(filepath.Join(root, "twd-*"))
	if len(ds) != 1 {
		return "", fail("bad_request", tgz+" is not a twd release tarball")
	}
	return ds[0], nil
}

// backupMgmt copies the store (VACUUM INTO) and the rest of mgmt's state
// that must match it (custody, issuer and ID secrets, node state), plus
// twd.json, into <state>/backups/<time>-<version>.
func backupMgmt(ctx context.Context, s *state, cur string) (string, error) {
	dst := filepath.Join(s.Dir, "backups", time.Now().UTC().Format("20060102T150405Z")+"-"+cur)
	if err := os.MkdirAll(filepath.Join(dst, "mgmt"), 0o700); err != nil {
		return "", err
	}
	src := filepath.Join(s.Dir, "mgmt")
	if _, err := os.Stat(filepath.Join(src, "mgmt.db")); err == nil {
		if err := mgmt.BackupSQLite(ctx, filepath.Join(src, "mgmt.db"), filepath.Join(dst, "mgmt", "mgmt.db")); err != nil {
			return "", fmt.Errorf("store: %w", err)
		}
	}
	skip := map[string]bool{"mgmt.db": true, "mgmt.db-wal": true, "mgmt.db-shm": true, "registry": true, "releases": true, "logs": true}
	ents, _ := os.ReadDir(src)
	for _, e := range ents {
		if skip[e.Name()] {
			continue
		}
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, "mgmt", e.Name())); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "twd.json")); err == nil {
		if err := copyTree(filepath.Join(s.Dir, "twd.json"), filepath.Join(dst, "twd.json")); err != nil {
			return "", err
		}
	}
	return dst, nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(t, 0o700)
		case info.Mode().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(t, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
			if err != nil {
				return err
			}
			_, err = io.Copy(out, in)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			return err
		}
		return nil // sockets, symlinks: not state
	})
}

// restoreMgmt puts a backup back (twd must be stopped).
func restoreMgmt(s *state, backup string) error {
	dst := filepath.Join(s.Dir, "mgmt")
	for _, f := range []string{"mgmt.db", "mgmt.db-wal", "mgmt.db-shm"} {
		os.Remove(filepath.Join(dst, f))
	}
	ents, err := os.ReadDir(filepath.Join(backup, "mgmt"))
	if err != nil {
		return err
	}
	for _, e := range ents {
		os.RemoveAll(filepath.Join(dst, e.Name()))
		if err := copyTree(filepath.Join(backup, "mgmt", e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(backup, "twd.json")); err == nil {
		return copyTree(filepath.Join(backup, "twd.json"), filepath.Join(s.Dir, "twd.json"))
	}
	return nil
}

func restartMode(s *state) (service, running bool) {
	inst, act := serviceOf(s)
	if inst && act {
		return true, false
	}
	return false, s.supervisorAlive()
}

func stopTwd(s *state, prefix string, service, running bool) error {
	switch {
	case service:
		return serviceStop()
	case running:
		out, err := exec.Command(filepath.Join(prefix, "bin", "twd"), "down", "--state-dir", s.Dir).CombinedOutput()
		if err != nil {
			return fail("upgrade_failed", "twd down: "+err.Error(), lastLinesOf(string(out), 5)...)
		}
	}
	return nil
}

func startTwd(s *state, prefix string, service, running bool) error {
	switch {
	case service:
		if runtime.GOOS == "darwin" {
			_, err := launchctl("kickstart", "-k", launchdDomain()+"/"+launchdLabel)
			return err
		}
		_, err := systemctlUser("restart", serviceUnit)
		return err
	case running:
		cmd := exec.Command(filepath.Join(prefix, "bin", "twd"), "up", "--yes", "--state-dir", s.Dir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("twd up: %v: %s", err, strings.Join(lastLinesOf(string(out), 3), " | "))
		}
	}
	return nil
}

// waitHealthy waits for mgmt to answer with every replica and host on want
// and every deployment that was active before (before) active again.
func waitHealthy(s0 *state, want string, timeout time.Duration, before deploymentsSnapshot) error {
	deadline := time.Now().Add(timeout)
	last := "not answering"
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		s, err := openState(s0.Dir)
		if err != nil {
			last = err.Error()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var st struct {
			OK       bool               `json:"ok"`
			Versions mgmt.VersionStatus `json:"versions"`
		}
		err = s.mgmt().Do(ctx, "GET", "/v1/status", nil, &st)
		cancel()
		if err != nil {
			last = "mgmt: " + err.Error()
			continue
		}
		if why := versionsAt(st.Versions, want); why != "" {
			last = why
			continue
		}
		if !st.OK {
			last = "GET /v1/status is not ok"
			continue
		}
		if why := deploymentsActive(s, before); why != "" {
			last = why
			continue
		}
		return nil
	}
	return fmt.Errorf("%s after %v", last, timeout)
}

func versionsAt(vs mgmt.VersionStatus, want string) string {
	wv, err := twdversion.Parse(want)
	if err != nil {
		return ""
	}
	for id, v := range vs.Mgmt {
		if sv, err := twdversion.Parse(v); err != nil || sv.Compare(wv) != 0 {
			return "mgmt replica " + id + " runs " + v
		}
	}
	for v, n := range vs.Hosts {
		if sv, err := twdversion.Parse(v); err != nil || sv.Compare(wv) != 0 {
			return fmt.Sprintf("%d host(s) run %q", n, v)
		}
	}
	return ""
}

func rollback(ctx context.Context, s *state, prefix, backup string, service, running bool) error {
	_ = stopTwd(s, prefix, service, running)
	if err := restoreMgmt(s, backup); err != nil {
		return fmt.Errorf("restore %s: %w", backup, err)
	}
	if out, err := exec.CommandContext(ctx, "sh", filepath.Join(prefix, "install.sh"), "--prefix", prefix, "--rollback", "--force").CombinedOutput(); err != nil {
		return fmt.Errorf("install.sh --rollback: %v: %s", err, strings.Join(lastLinesOf(string(out), 3), " | "))
	}
	// The store went back in time: no lease epoch may be reused. Done here,
	// not by the restored release's twd-mgmt (an older one has no
	// restore-fence), and without migrating the restored store.
	if db := filepath.Join(s.Dir, "mgmt", "mgmt.db"); fileExists(db) {
		if _, err := mgmt.RestoreFenceDSN(ctx, db); err != nil {
			return fmt.Errorf("restore fence: %w", err)
		}
	}
	return startTwd(s, prefix, service, running)
}

func lastLinesOf(s string, n int) []string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return ls
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// mergeEntries is a ∪ b by version (a wins), sorted.
func mergeEntries(a, b []catalog.Entry) []catalog.Entry {
	out := slices.Clone(a)
	for _, e := range b {
		if _, ok := catalog.Find(out, e.Version); !ok {
			out = append(out, e)
		}
	}
	catalog.Sort(out)
	return out
}
