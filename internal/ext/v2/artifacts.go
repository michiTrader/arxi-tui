package extv2

import (
	"fmt"
	"strings"
)

// This file validates the three artifacts that run without the user typing anything:
// events, panels and services. They share one rule, the same as the rest of the contract:
// what runs by itself can only show, never act for the user. So an automatic operation
// says text and nothing else (no send, no rewrite of a line), it has no placeholder
// because no text was typed, and a service starts only when the user types /service start.

func (p *Package) validateAutomatic(opt Options) error {
	if len(p.Events) > MaxEvents {
		return fmt.Errorf("events: %d defined, the most is %d; remove some or split the package", len(p.Events), MaxEvents)
	}
	if len(p.Panels) > MaxPanels {
		return fmt.Errorf("panels: %d defined, the most is %d; remove some or split the package", len(p.Panels), MaxPanels)
	}
	if len(p.Services) > MaxServices {
		return fmt.Errorf("services: %d defined, the most is %d; remove some or split the package", len(p.Services), MaxServices)
	}
	ids := map[string]bool{}
	for i, e := range p.Events {
		if err := p.validateEvent(i, e, opt); err != nil {
			return err
		}
		if ids[e.ID] {
			return fmt.Errorf("events: id %q is defined twice, so a patch could not tell them apart; give each event its own id", e.ID)
		}
		ids[e.ID] = true
	}
	panels := map[string]Panel{}
	for i, pn := range p.Panels {
		if err := p.validatePanel(i, pn, opt); err != nil {
			return err
		}
		if _, dup := panels[pn.ID]; dup {
			return fmt.Errorf("panels: id %q is defined twice, so one of them would never be shown; give each panel its own id", pn.ID)
		}
		panels[pn.ID] = pn
	}
	names := map[string]bool{}
	fed := map[string]string{}
	for i, sv := range p.Services {
		if err := p.validateService(i, sv, panels, opt); err != nil {
			return err
		}
		if names[sv.Name] {
			return fmt.Errorf("services: %s is defined twice, so /service start could not tell them apart; keep one", sv.Name)
		}
		names[sv.Name] = true
		if sv.Feed != "" {
			if other, taken := fed[sv.Feed]; taken {
				return fmt.Errorf("service %s: panel %q is already filled by service %s, and two programs writing one line would overwrite each other; feed another panel", sv.Name, sv.Feed, other)
			}
			fed[sv.Feed] = sv.Name
		}
	}
	for _, pn := range p.Panels {
		if pn.Op == nil && fed[pn.ID] == "" {
			return fmt.Errorf("panel %s: it has no op and no service feeds it, so it would stay empty; add an op, or name it in a service's \"feed\"", pn.ID)
		}
	}
	return nil
}

func (p *Package) validateEvent(i int, e Event, opt Options) error {
	who := fmt.Sprintf("events[%d]", i)
	if len(e.ID) > MaxNameLen || !ruleIDRe.MatchString(e.ID) {
		return fmt.Errorf("%s: id %q is not usable; use lowercase letters, digits, '.', '-' or '_', starting with a letter", who, e.ID)
	}
	who = "event " + e.ID
	if !contains(EventKinds, e.On) {
		return fmt.Errorf("%s: on %q is not something a package can react to, so it would never run; the events are %s", who, e.On, strings.Join(EventKinds, ", "))
	}
	if e.Is != "" {
		if !IsSettingEvent(e.On) {
			return fmt.Errorf("%s: \"is\" narrows a setting to one value, and %s is not a setting, so it would never match; remove \"is\"", who, e.On)
		}
		if opt.EventValues != nil {
			vals, ok := opt.EventValues(e.On)
			if ok && !contains(vals, e.Is) {
				return fmt.Errorf("%s: %s is never %q, so the event would never run; its values are %s (leave \"is\" out to react to any)", who, e.On, e.Is, strings.Join(vals, ", "))
			}
		}
	}
	return p.validateOp(who, e.Op, opCtx{kind: ctxEvent}, opt)
}

func (p *Package) validatePanel(i int, pn Panel, opt Options) error {
	who := fmt.Sprintf("panels[%d]", i)
	if len(pn.ID) > MaxNameLen || !cmdNameRe.MatchString(pn.ID) {
		return fmt.Errorf("%s: id %q is not usable; use lowercase letters, digits and '-', starting with a letter", who, pn.ID)
	}
	who = "panel " + pn.ID
	if err := checkLabel(who+" title", pn.Title, MaxPanelTitle, true); err != nil {
		return err
	}
	switch pn.Refresh {
	case "", EventTurnDone:
	default:
		return fmt.Errorf("%s: refresh %q is not understood; use \"turn.done\" (after each answer) or leave it out", who, pn.Refresh)
	}
	if pn.EveryS != 0 && (pn.EveryS < MinPanelEveryS || pn.EveryS > MaxPanelEveryS) {
		return fmt.Errorf("%s: every_s %d is outside %d..%d; a faster panel would run a program or a request all the time", who, pn.EveryS, MinPanelEveryS, MaxPanelEveryS)
	}
	if pn.Op == nil {
		if pn.Refresh != "" || pn.EveryS != 0 {
			return fmt.Errorf("%s: it has no op, so refresh and every_s have nothing to run; a service fills this panel, or add an op", who)
		}
		return nil
	}
	return p.validateOp(who, *pn.Op, opCtx{kind: ctxPanel}, opt)
}

func (p *Package) validateService(i int, sv Service, panels map[string]Panel, opt Options) error {
	who := fmt.Sprintf("services[%d]", i)
	if len(sv.Name) > MaxNameLen || !cmdNameRe.MatchString(sv.Name) {
		return fmt.Errorf("%s: name %q is not usable; it is typed after /service start, so use lowercase letters, digits and '-', starting with a letter", who, sv.Name)
	}
	who = "service " + sv.Name
	if err := checkLabel(who+" description", sv.Description, MaxDescLen, true); err != nil {
		return err
	}
	if sv.Feed != "" {
		pn, ok := panels[sv.Feed]
		if !ok {
			return fmt.Errorf("%s: feed %q is not a panel of this package, so its output would go nowhere; name one of its panels", who, sv.Feed)
		}
		if pn.Op != nil {
			return fmt.Errorf("%s: panel %q has an op of its own, and two writers on one line would overwrite each other; remove the panel's op or the feed", who, sv.Feed)
		}
	}
	o := sv.Op
	if o.Kind != KindExec {
		return fmt.Errorf("%s: a service is a program that keeps running, so its op is \"exec\"; a request that ends belongs in a command, event or panel", who)
	}
	if o.TimeoutMS != 0 {
		return fmt.Errorf("%s: timeout_ms is for an operation that ends, and a service runs until the user stops it; remove it", who)
	}
	if o.Extract != "" {
		return fmt.Errorf("%s: extract reads one whole answer, and a service prints lines; remove it", who)
	}
	return p.validateOp(who, o, opCtx{kind: ctxService}, opt)
}
