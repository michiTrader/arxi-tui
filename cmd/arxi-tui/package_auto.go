package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
)

// What a package does by itself (events, panels, services). The contract and the
// runner are in internal/ext/v2 and narrow all of it to showing text; this file is only
// the host side: when to fire, where the text goes, and how the user starts and stops a
// service. Nothing here lets an event send to the model, change a setting or start
// another event: the runner returns text and this file only displays it.

// autoMsg is what a background job hands the loop. The zero value means "something on
// screen changed, repaint" (a panel took a new line).
type autoMsg struct {
	Note, Warn string
}

// autoJobTimeout bounds one event or panel run on top of the operation's own timeout.
const autoJobTimeout = 60 * time.Second

// maxRunningServices is how many services the user may have running at once.
const maxRunningServices = 8

// autoPanel is the one line a package keeps on screen.
type autoPanel struct {
	pkg, id, title, hash, refresh string
	text                          string
	every                         time.Duration
	next                          time.Time
	hasOp                         bool
}

func (p *autoPanel) key() string { return p.pkg + "/" + p.id }

// autoService is a service the user started.
type autoService struct {
	key, pkg, name, hash string
	h                    *extv2.ServiceHandle
	started              time.Time
	byUser               bool // the user stopped it, so its end needs no word
}

// packageAuto runs the events, panels and services of the packages that may run. All
// jobs run on their own goroutines; they speak to the loop only through out.
type packageAuto struct {
	ctx context.Context
	out chan<- autoMsg
	now func() time.Time

	mu       sync.Mutex
	pkgs     packageSet
	runner   *extv2.Runner
	panels   map[string]*autoPanel
	order    []string
	jobs     map[string]bool
	services map[string]*autoService
	lastErr  map[string]string
	wg       sync.WaitGroup
}

func newPackageAuto(ctx context.Context, out chan<- autoMsg) *packageAuto {
	return &packageAuto{
		ctx: ctx, out: out, now: time.Now,
		panels: map[string]*autoPanel{}, jobs: map[string]bool{},
		services: map[string]*autoService{}, lastErr: map[string]string{},
	}
}

// deliver hands a message to the loop without ever blocking the job: the channel is
// buffered, and when it is full the message waits on its own goroutine.
func (a *packageAuto) deliver(m autoMsg) {
	select {
	case a.out <- m:
	default:
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			select {
			case a.out <- m:
			case <-a.ctx.Done():
			}
		}()
	}
}

// reload takes the packages as they are now. A panel keeps its line when its package did
// not change; a service whose package was revoked, switched off or edited is stopped
// (the approval was for the old version). Panels that have an operation run once now.
func (a *packageAuto) reload(s packageSet, r *extv2.Runner) {
	a.mu.Lock()
	old := a.panels
	a.pkgs, a.runner = s, r
	a.panels, a.order = map[string]*autoPanel{}, nil
	now := a.now()
	for _, p := range s.pkgs {
		if !p.runnable() {
			continue
		}
		for _, pn := range p.Panels {
			if pn.Disabled {
				continue
			}
			ap := &autoPanel{pkg: p.ID, id: pn.ID, title: cleanWarnText(pn.Title), hash: p.Hash,
				refresh: pn.Refresh, hasOp: pn.Op != nil}
			if pn.EveryS > 0 {
				ap.every = time.Duration(pn.EveryS) * time.Second
				ap.next = now.Add(ap.every)
			}
			if o := old[ap.key()]; o != nil && o.hash == p.Hash {
				ap.text = o.text
			}
			a.panels[ap.key()] = ap
			a.order = append(a.order, ap.key())
		}
	}
	var stopped []string
	for k, sv := range a.services {
		if !a.serviceStillAllowed(sv) {
			sv.byUser = true
			sv.h.Stop()
			delete(a.services, k)
			stopped = append(stopped, k)
		}
	}
	a.mu.Unlock()
	sort.Strings(stopped)
	for _, k := range stopped {
		a.deliver(autoMsg{Note: "service " + cleanWarnText(k) + " was stopped: its package is no longer approved as it was."})
	}
	a.refreshPanels(func(p *autoPanel) bool { return p.hasOp })
}

// serviceStillAllowed reports whether the package of a running service is still the
// approved version, with the service still declared and not switched off. Caller holds mu.
func (a *packageAuto) serviceStillAllowed(sv *autoService) bool {
	p, ok := a.pkgs.find(sv.pkg)
	if !ok || !p.runnable() || p.Hash != sv.hash {
		return false
	}
	for _, s := range p.Services {
		if s.Name == sv.name && !s.Disabled {
			return true
		}
	}
	return false
}

// begin marks a job as running and reports false when the same job already is, so a slow
// program is never started again on top of itself.
func (a *packageAuto) begin(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jobs[key] {
		return false
	}
	a.jobs[key] = true
	return true
}

func (a *packageAuto) end(key string) {
	a.mu.Lock()
	delete(a.jobs, key)
	a.mu.Unlock()
}

// goJob runs fn on its own goroutine; a crash in a builtin is that job's error.
func (a *packageAuto) goJob(key string, fn func(ctx context.Context) error, done func(err error)) {
	if !a.begin(key) {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.end(key)
		ctx, cancel := context.WithTimeout(a.ctx, autoJobTimeout)
		defer cancel()
		var err error
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("it crashed (%v)", rec)
				}
			}()
			err = fn(ctx)
		}()
		if a.ctx.Err() != nil {
			return
		}
		done(err)
	}()
}

// fire runs the events that react to on (and to value, for a setting). value is ignored
// for turn events.
func (a *packageAuto) fire(on, value string) {
	type job struct {
		p installedPackage
		e extv2.Event
	}
	a.mu.Lock()
	var jobs []job
	r := a.runner
	for _, p := range a.pkgs.pkgs {
		if !p.runnable() {
			continue
		}
		for _, e := range p.Events {
			if e.Disabled || e.On != on {
				continue
			}
			if extv2.IsSettingEvent(on) && e.Is != "" && e.Is != value {
				continue
			}
			jobs = append(jobs, job{p, e})
		}
	}
	a.mu.Unlock()
	if r == nil {
		return
	}
	for _, j := range jobs {
		j := j
		who := cleanWarnText(j.p.ID + "/" + j.e.ID)
		var text string
		a.goJob("event:"+j.p.ID+"/"+j.e.ID, func(ctx context.Context) (err error) {
			text, err = r.RunEvent(ctx, j.p.Package, j.e)
			return err
		}, func(err error) {
			switch {
			case err != nil:
				a.deliver(autoMsg{Warn: who + ": " + cleanWarnText(err.Error())})
			case strings.TrimSpace(text) != "":
				a.deliver(autoMsg{Note: who + ": " + text})
			}
		})
	}
}

// turnDone and turnFailed are the ends of a turn the user asked for.
func (a *packageAuto) turnDone() {
	a.fire(extv2.EventTurnDone, "")
	a.refreshPanels(func(p *autoPanel) bool { return p.hasOp && p.refresh == extv2.EventTurnDone })
}

func (a *packageAuto) turnFailed() { a.fire(extv2.EventTurnFailed, "") }

// refreshPanels runs the operation of every panel pick selects.
func (a *packageAuto) refreshPanels(pick func(*autoPanel) bool) {
	type job struct {
		key string
		p   installedPackage
		pn  extv2.Panel
	}
	a.mu.Lock()
	r := a.runner
	var jobs []job
	for _, k := range a.order {
		ap := a.panels[k]
		if ap == nil || !pick(ap) {
			continue
		}
		p, ok := a.pkgs.find(ap.pkg)
		if !ok {
			continue
		}
		for _, pn := range p.Panels {
			if pn.ID == ap.id && pn.Op != nil {
				jobs = append(jobs, job{k, p, pn})
			}
		}
	}
	a.mu.Unlock()
	if r == nil {
		return
	}
	for _, j := range jobs {
		j := j
		var line string
		a.goJob("panel:"+j.key, func(ctx context.Context) (err error) {
			line, err = r.RunPanel(ctx, j.p.Package, j.pn)
			return err
		}, func(err error) {
			a.mu.Lock()
			ap := a.panels[j.key]
			if ap == nil || ap.hash != j.p.Hash {
				a.mu.Unlock()
				return // reloaded meanwhile: this answer is for a package that is gone
			}
			msg := autoMsg{}
			if err != nil {
				ap.text = "(failed)"
				if why := cleanWarnText(err.Error()); a.lastErr[j.key] != why {
					a.lastErr[j.key] = why
					msg.Warn = "panel " + cleanWarnText(j.key) + ": " + why
				}
			} else {
				ap.text = line
				delete(a.lastErr, j.key)
			}
			a.mu.Unlock()
			a.deliver(msg)
		})
	}
}

// hasTimed reports whether any panel refreshes on a clock, so the loop only keeps a timer
// while one needs it.
func (a *packageAuto) hasTimed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.panels {
		if p.every > 0 && p.hasOp {
			return true
		}
	}
	return false
}

// tick refreshes the panels whose time has come.
func (a *packageAuto) tick() {
	now := a.now()
	a.mu.Lock()
	due := map[string]bool{}
	for k, p := range a.panels {
		if p.every > 0 && p.hasOp && !now.Before(p.next) {
			due[k] = true
			p.next = now.Add(p.every)
		}
	}
	a.mu.Unlock()
	if len(due) > 0 {
		a.refreshPanels(func(p *autoPanel) bool { return due[p.key()] })
	}
}

// panelsLine is host.panels: every panel that has a line, "Title: text", on one row.
func (a *packageAuto) panelsLine() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var parts []string
	for _, k := range a.order {
		if p := a.panels[k]; p != nil && p.text != "" {
			parts = append(parts, p.title+": "+p.text)
		}
	}
	return strings.Join(parts, " · ")
}

// ---- services ------------------------------------------------------------------

// serviceChoice is a service a package declares.
type serviceChoice struct {
	p  installedPackage
	sv extv2.Service
}

// findServices lists the services of the packages that may run, matching ref ("name" or
// "pkg/name"); an empty ref matches all.
func (a *packageAuto) findServices(ref string) []serviceChoice {
	var out []serviceChoice
	for _, p := range a.pkgs.pkgs {
		if !p.runnable() {
			continue
		}
		for _, sv := range p.Services {
			if sv.Disabled {
				continue
			}
			if ref == "" || ref == sv.Name || ref == p.ID+"/"+sv.Name {
				out = append(out, serviceChoice{p, sv})
			}
		}
	}
	return out
}

// listServices says what each declared service is doing.
func (a *packageAuto) listServices() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	all := a.findServices("")
	if len(all) == 0 {
		return "No approved package declares a service. A service is a program a package ships that keeps running until you stop it; /package lists the packages."
	}
	var b strings.Builder
	b.WriteString("Services:")
	for _, c := range all {
		key := c.p.ID + "/" + c.sv.Name
		state := "stopped"
		if sv := a.services[key]; sv != nil {
			state = "running " + shellAge(a.now().Sub(sv.started))
		}
		fmt.Fprintf(&b, "\n  %s · %s · %s", cleanWarnText(key), state, cleanWarnText(c.sv.Description))
	}
	b.WriteString("\n/service start <name>, /service stop <name|all>")
	return b.String()
}

// startService starts the service ref names. synth says the line was not typed by the
// user: a menu row, a button or a command or hook of their behaviour may not start a
// program that keeps running.
func (a *packageAuto) startService(ref string, synth bool) string {
	if synth {
		return "/service start only works when you type it yourself; a shortcut, button or command of the interface cannot start a program for you. Nothing started."
	}
	if ref == "" {
		return "/service start: name the service. /service lists them."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	found := a.findServices(ref)
	switch {
	case len(found) == 0:
		return "/service: no approved package declares a service called " + cleanWarnText(ref) + ". /service lists them."
	case len(found) > 1:
		var names []string
		for _, c := range found {
			names = append(names, c.p.ID+"/"+c.sv.Name)
		}
		return "/service: " + cleanWarnText(ref) + " is declared by more than one package; write one of " + cleanWarnText(strings.Join(names, ", ")) + "."
	}
	c := found[0]
	key := c.p.ID + "/" + c.sv.Name
	if a.services[key] != nil {
		return cleanWarnText(key) + " is already running."
	}
	if len(a.services) >= maxRunningServices {
		return fmt.Sprintf("/service: %d services are already running, the most there can be. /service stop <name> ends one.", maxRunningServices)
	}
	if a.runner == nil {
		return "/service: nothing can run in this session."
	}
	run := &autoService{key: key, pkg: c.p.ID, name: c.sv.Name, hash: c.p.Hash, started: a.now()}
	feed := c.sv.Feed
	h, err := a.runner.StartService(a.ctx, c.p.Package, c.sv, func(line string) {
		if feed != "" {
			a.mu.Lock()
			if ap := a.panels[c.p.ID+"/"+feed]; ap != nil && ap.hash == run.hash {
				ap.text = extv2.PanelLine(line)
			}
			a.mu.Unlock()
			a.deliver(autoMsg{})
			return
		}
		a.deliver(autoMsg{Note: cleanWarnText(key) + ": " + line})
	})
	if err != nil {
		return "/service: " + cleanWarnText(err.Error())
	}
	run.h = h
	a.services[key] = run
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		<-h.Done()
		a.mu.Lock()
		user := run.byUser
		if a.services[key] == run {
			delete(a.services, key)
		}
		a.mu.Unlock()
		if a.ctx.Err() != nil {
			return
		}
		var say []string
		if n := h.Dropped(); n > 0 {
			say = append(say, fmt.Sprintf("%d lines were dropped because it printed faster than they can be shown", n))
		}
		if err := h.Err(); err != nil {
			a.deliver(autoMsg{Warn: "service " + cleanWarnText(key) + ": " + cleanWarnText(err.Error()) + joinSay(say)})
		} else if !user {
			a.deliver(autoMsg{Note: "service " + cleanWarnText(key) + " ended." + joinSay(say)})
		}
	}()
	return "Started " + cleanWarnText(key) + ". /service stop " + cleanWarnText(c.sv.Name) + " ends it."
}

func joinSay(say []string) string {
	if len(say) == 0 {
		return ""
	}
	return " (" + strings.Join(say, "; ") + ")"
}

// stopService ends one service, or all of them.
func (a *packageAuto) stopService(ref string) string {
	if ref == "" {
		return "/service stop: name the service, or write all."
	}
	a.mu.Lock()
	var hit []*autoService
	for _, sv := range a.services {
		if ref == "all" || ref == sv.name || ref == sv.key {
			hit = append(hit, sv)
		}
	}
	for _, sv := range hit {
		sv.byUser = true
	}
	a.mu.Unlock()
	if len(hit) == 0 {
		return "/service stop: " + cleanWarnText(ref) + " is not running."
	}
	sort.Slice(hit, func(i, j int) bool { return hit[i].key < hit[j].key })
	var names []string
	for _, sv := range hit {
		sv.h.Stop()
		names = append(names, sv.key)
	}
	return "Stopped " + cleanWarnText(strings.Join(names, ", ")) + "."
}

// runningServices counts the services that run now.
func (a *packageAuto) runningServices() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.services)
}

// stopAll ends every service when the program closes.
func (a *packageAuto) stopAll() {
	a.mu.Lock()
	for _, sv := range a.services {
		sv.byUser = true
		sv.h.Stop()
	}
	a.mu.Unlock()
}

// ---- the /service line ----------------------------------------------------------

// serviceLine reports whether Enter on this line is `/service`, with what follows it.
func serviceLine(input string, sel int, cat string) (args string, ok bool) {
	if menuCommand("service", input, sel, cat) {
		return "", true
	}
	if rest, matched := stripLeadingVerb(strings.TrimSpace(input), "service"); matched && strings.HasPrefix(strings.TrimSpace(input), "/") {
		return rest, true
	}
	return "", false
}

// runServiceCommand carries out `/service ...` and returns what to show.
//
//	/service                 list
//	/service start <name>    start one (typed by the user only)
//	/service stop <name|all> end one, or all
func runServiceCommand(args string, a *packageAuto, synth bool) string {
	if a == nil {
		return "/service: this session cannot run packages."
	}
	f := strings.Fields(args)
	if len(f) == 0 || f[0] == "list" {
		return a.listServices()
	}
	switch f[0] {
	case "start":
		return a.startService(strings.Join(f[1:], " "), synth)
	case "stop":
		return a.stopService(strings.Join(f[1:], " "))
	}
	return "/service: use /service, /service start <name> or /service stop <name|all>"
}
