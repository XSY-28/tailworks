package main

import "github.com/rosskukulinski/tailworks/internal/npminstall"

// tailworks installed by npm (issue #240): `npm install -g tailworks` or
// `npx tailworks …`. The binaries live in the platform package
// (@tailworks/cli-<os>-<arch>), laid out like a tarball install, so tw
// finds its daemons, workerd and the bundler as usual. State stays in the
// state dir (~/.tailworks by default), as for a tarball install. What
// differs: npm owns the package directory, so tw upgrade refuses and says
// to use npm, and npx's cache is no place for a login service.

// npmSelf is the platform package this tw runs from, if npm installed it.
var npmSelf = npminstall.Self

// npmUpgradeErr is tw upgrade's refusal for an npm install.
func npmUpgradeErr(in npminstall.Install) *cliErr {
	e := fail("npm_install", "this tailworks was installed by "+in.Kind()+" ("+in.Package+" "+in.Version+" in "+in.Dir+"): npm owns that directory, so tw upgrade won't replace it")
	if in.NPX {
		return e.withFix("tw down, then npx tailworks@latest up …   (npx runs the newest release)",
			"or install it for good: "+npminstall.UpgradeCommand)
	}
	return e.withFix("tw down && "+npminstall.UpgradeCommand+" && tw up   (tw backup first: npm has no rollback)",
		"in a project that installed it: npm install tailworks@latest there")
}

// npmServiceErr refuses a login service that would run tw from npx's
// cache, which npm may clear at any time; nil otherwise.
func npmServiceErr() error {
	in, ok := npmSelf()
	if !ok || !in.NPX {
		return nil
	}
	return fail("npm_install", "this tailworks runs from npx's cache ("+in.Dir+"), which npm may clear at any time: a login service can't point there").
		withFix(npminstall.UpgradeCommand+"   then tw up --service (a global install has a stable path)",
			"or tw up --service=false   (runs in the background until you log out or tw down)")
}

// npmUpgradeCommand is version --check's upgrade hint for an npm install,
// or "".
func npmUpgradeCommand() string {
	in, ok := npmSelf()
	if !ok {
		return ""
	}
	if in.NPX {
		return "npx tailworks@latest …"
	}
	return npminstall.UpgradeCommand
}

// doctorInstall is the "install" check: how this tailworks was installed,
// and what that means for upgrades and the microVM tier.
func doctorInstall() (string, string, string) {
	if in, ok := npmSelf(); ok {
		d := "npm package " + in.Package + " " + in.Version + " (" + in.Kind() + ") in " + in.Dir +
			"; upgrade with npm, not tw upgrade; the npm package has no microVM guest (process tier only: the microVM tier needs the release tarball, scripts/get.sh)"
		if in.NPX {
			return "info", d + "; npx's cache can be cleared at any time, so tw up runs without a login service", "for a login service: " + npminstall.UpgradeCommand
		}
		return "ok", d, ""
	}
	if p := installPrefixOf(); p != "" {
		return "ok", "release tarball (install.sh) in " + p + "; tw upgrade upgrades it", ""
	}
	if root := sourceCheckout(); root != "" {
		return "info", "a source checkout (" + root + "): make build", ""
	}
	return "info", "tw runs from " + exeDir() + " (not an install.sh prefix or npm package): tw upgrade can't replace it", ""
}
