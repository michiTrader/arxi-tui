package main

import (
	"fmt"
	"strings"
)

// /package is how the user sees the packages the host found and decides which of them
// may run. Nothing here changes what a package can do; it records a decision.
//
//	/package                      the list, and why a package is not running
//	/package show <id>            what the package is able to do, in plain words
//	/package approve <id>         shows the same, and the code to type to approve it
//	/package approve <id> <code>  approves exactly the version the code was shown for
//	/package revoke <id>          withdraws the approval
//
// The code is the start of the hash a grant is bound to (packages.go), so approving is a
// second, deliberate act on a version the user was just shown. It also means the
// approval cannot be produced by a menu row, a button or a command of the user's own
// behaviour, which all travel the loop as synthesised lines: those are refused outright.

const packageCodeLen = 10

// packageCommand is one parsed /package line.
type packageCommand struct {
	Verb string // list, show, approve, revoke, usage
	ID   string
	Code string
}

// packageLineOf reads what Enter means on the input line. ok is false for any other line.
func packageLineOf(input string, sel int, cat string) (packageCommand, bool) {
	if menuCommand("package", input, sel, cat) {
		return packageCommand{Verb: "list"}, true
	}
	f := strings.Fields(input)
	if len(f) == 0 || f[0] != "/package" {
		return packageCommand{}, false
	}
	f = f[1:]
	switch {
	case len(f) == 0:
		return packageCommand{Verb: "list"}, true
	case f[0] == "list" && len(f) == 1:
		return packageCommand{Verb: "list"}, true
	case (f[0] == "show" || f[0] == "revoke") && len(f) == 2:
		return packageCommand{Verb: f[0], ID: f[1]}, true
	case f[0] == "approve" && (len(f) == 2 || len(f) == 3):
		c := packageCommand{Verb: "approve", ID: f[1]}
		if len(f) == 3 {
			c.Code = f[2]
		}
		return c, true
	}
	return packageCommand{Verb: "usage"}, true
}

const packageUsage = "/package: use /package, /package show <id>, /package approve <id> or /package revoke <id>"

// packageState says in a word whether the package runs and, if not, why.
func packageState(p installedPackage, grants map[string]string, cwd string) string {
	switch {
	case p.Disabled:
		return "switched off"
	case p.Granted:
		return "approved"
	}
	if _, had := grants[grantKey(p.Scope, cwd, p.ID)]; had {
		return "changed since you approved it, not running"
	}
	return "not approved, not running"
}

func packageCode(p installedPackage) string {
	if len(p.Hash) < packageCodeLen {
		return p.Hash
	}
	return p.Hash[:packageCodeLen]
}

// packageListText is what /package prints.
func packageListText(s packageSet, grants map[string]string, cwd string) string {
	var b strings.Builder
	if len(s.pkgs) == 0 {
		b.WriteString("No packages. They live in " + packagesWhere(cwd) + ".")
	} else {
		fmt.Fprintf(&b, "Packages (%d):", len(s.pkgs))
		for _, p := range s.pkgs {
			cmds := ""
			for _, c := range p.Commands {
				cmds += " /" + c.Name
			}
			rules := ""
			if n := len(p.Rules); n > 0 {
				rules = fmt.Sprintf(" · %d rule(s)", n)
			}
			if n := len(p.Tools); n > 0 {
				rules += fmt.Sprintf(" · %d tool(s) the model may call", n)
			}
			fmt.Fprintf(&b, "\n  %s %s (%s) · %s%s%s", cleanWarnText(p.ID), cleanWarnText(p.Version), p.Scope, packageState(p, grants, cwd), cmds, rules)
		}
		b.WriteString("\n/package show <id> says what one can do.")
	}
	for _, pr := range s.problems {
		b.WriteString("\n! " + pr)
	}
	return b.String()
}

func packagesWhere(cwd string) string {
	where := userPackagesDir()
	if where == "" {
		where = "the config folder"
	}
	if d := projectPackagesDir(cwd); d != "" {
		where += " or " + d
	}
	return where
}

// packageShowText is the consent text: what the package is and everything it can do.
func packageShowText(p installedPackage, grants map[string]string, cwd string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s %s (%s)", cleanWarnText(p.ID), cleanWarnText(p.Name), cleanWarnText(p.Version), p.Scope)
	fmt.Fprintf(&b, "\nState: %s", packageState(p, grants, cwd))
	caps := p.Capabilities()
	if len(caps) == 0 {
		b.WriteString("\nIt can only show text and use what the app itself offers.")
	} else {
		b.WriteString("\nIt can:")
		for _, c := range caps {
			b.WriteString("\n  - " + cleanWarnText(c))
		}
	}
	for _, c := range p.Commands {
		line := "\nCommand /" + cleanWarnText(c.Name)
		if c.Description != "" {
			line += ": " + cleanWarnText(c.Description)
		}
		b.WriteString(line)
	}
	for _, t := range p.Tools {
		line := "\nTool " + cleanWarnText(t.ModelName(p.ID))
		if t.Description != "" {
			line += ": " + cleanWarnText(t.Description)
		}
		b.WriteString(line + " (the model may call it by itself)")
	}
	for _, r := range p.Rules {
		b.WriteString("\nRule " + cleanWarnText(r.ID) + " on " + cleanWarnText(r.Stage))
	}
	if p.Dir != "" {
		b.WriteString("\nIt is a folder: approving covers every file in " + cleanWarnText(p.Dir) + ", and changing any of them asks again.")
	}
	return b.String()
}

// packageDecision is what running a /package line did, for the loop to show.
type packageDecision struct {
	Note, Warn string
	Changed    bool // the grants changed: the loop must reload the packages
}

// runPackageCommand carries out one line. synth says the line was not typed by the
// user (a menu row, a button, a command or hook of their behaviour): those may look but
// never decide.
func runPackageCommand(c packageCommand, s packageSet, cwd string, synth bool) packageDecision {
	grants := readGrants()
	find := func() (installedPackage, string) {
		p, ok := s.find(c.ID)
		if !ok {
			return installedPackage{}, "/package: there is no package called " + cleanWarnText(c.ID) + ". /package lists them."
		}
		return p, ""
	}
	switch c.Verb {
	case "list":
		return packageDecision{Note: packageListText(s, grants, cwd)}
	case "show":
		p, why := find()
		if why != "" {
			return packageDecision{Warn: why}
		}
		return packageDecision{Note: packageShowText(p, grants, cwd)}
	case "approve", "revoke":
		if synth {
			return packageDecision{Warn: "/package " + c.Verb + " only works when you type it yourself; a shortcut, button or command of the interface cannot decide this for you. Nothing changed."}
		}
		p, why := find()
		if why != "" {
			return packageDecision{Warn: why}
		}
		if c.Verb == "revoke" {
			had, err := revokePackage(p.Scope, cwd, p.ID)
			switch {
			case err != nil:
				return packageDecision{Warn: "/package: " + cleanWarnText(err.Error())}
			case !had:
				return packageDecision{Note: cleanWarnText(p.ID) + " had no approval to withdraw."}
			}
			return packageDecision{Note: cleanWarnText(p.ID) + " is no longer approved and will not run.", Changed: true}
		}
		if c.Code == "" {
			return packageDecision{Note: packageShowText(p, grants, cwd) + "\nTo approve exactly this version, type: /package approve " + cleanWarnText(p.ID) + " " + packageCode(p)}
		}
		if c.Code != packageCode(p) {
			return packageDecision{Warn: "/package: that code is not the one for this version of " + cleanWarnText(p.ID) + " (it may have changed since you looked). Nothing was approved. /package approve " + cleanWarnText(p.ID) + " shows the current one."}
		}
		if err := grantPackage(p, cwd); err != nil {
			return packageDecision{Warn: "/package: " + cleanWarnText(err.Error())}
		}
		return packageDecision{Note: cleanWarnText(p.ID) + " is approved. Its commands are in the / menu.", Changed: true}
	}
	return packageDecision{Warn: packageUsage}
}
