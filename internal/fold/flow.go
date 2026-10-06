package fold

// This file holds the part of the fold that the /flow screen reads: which stage
// the run went through, who waits on whom, and the single most important thing
// that currently stops the run. All of it is a pure function of the event log,
// exactly like the rest of State, so a replayed session draws the same screen
// as a live one.

// StageProgress is one stage the log proves the run entered, in the order it
// entered them. It records what happened, not what the blueprint plans: the
// stages that are still ahead are not here because no event names them.
type StageProgress struct {
	Name  string
	Index int
	// Submitted is the members that submitted to this stage, in log order.
	Submitted []string
	// Left is true once a stage.advanced moved the run on from this stage.
	Left bool
}

// Attention is the one blocker the user needs to see when "nothing is
// happening". Kind is the core's own vocabulary where one exists (approval,
// lock, peer, budget, timer, tool, workspace) plus two notices that are not
// agent.blocked: quiescent and budget_exceeded.
type Attention struct {
	Kind  string
	Actor string
	// Text is a ready, one-line description. For quiescent it is the core's
	// diagnosis verbatim: the core wrote it to name the concrete cause, and a
	// paraphrase would lose exactly that.
	Text string
	// Ref is the blocked_ref the core attached, kept whole so the host can
	// build the remedy (inbox_id for an approval, key for a lock).
	Ref map[string]any
}

// attnRec is an open attention item plus the order it arrived in.
type attnRec struct {
	Attention
}

// openAttention appends an item and returns nothing; the newest open item is
// the one shown, because it is the most recent thing that stopped the run.
func (s *State) openAttention(a Attention) {
	s.attn = append(s.attn, attnRec{a})
}

// closeAttention removes the first open item matching kind and actor. An
// empty actor matches any.
func (s *State) closeAttention(kind, actor string) {
	for i, r := range s.attn {
		if r.Kind == kind && (actor == "" || r.Actor == actor) {
			s.attn = append(s.attn[:i], s.attn[i+1:]...)
			return
		}
	}
}

// dropAttentionKinds removes every open item whose kind is listed.
func (s *State) dropAttentionKinds(kinds ...string) {
	kept := s.attn[:0]
	for _, r := range s.attn {
		drop := false
		for _, k := range kinds {
			if r.Kind == k {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, r)
		}
	}
	s.attn = kept
}

// blockedText describes an agent.blocked in one line from what the core sent.
func blockedText(actor, on string, ref map[string]any, task string) string {
	who := actor
	if who == "" {
		who = "a member"
	}
	switch on {
	case "approval":
		if tool := str(ref, "tool"); tool != "" {
			return who + " waits for approval: " + tool
		}
		return who + " waits for approval"
	case "peer":
		if p := str(ref, "peer"); p != "" {
			return who + " waits for " + p
		}
		return who + " waits for another member"
	case "lock":
		if k := str(ref, "key"); k != "" {
			if h := str(ref, "holder"); h != "" {
				return who + " waits for lock " + k + " held by " + h
			}
			return who + " waits for lock " + k
		}
		return who + " waits for a lock"
	case "budget":
		return who + " is stopped: the budget is spent"
	case "timer":
		return who + " waits for a timer"
	case "tool":
		if t := str(ref, "tool"); t != "" {
			return who + " waits for tool " + t
		}
		return who + " waits for a tool"
	case "workspace":
		if p := str(ref, "path"); p != "" {
			return who + " waits for the workspace " + p
		}
		return who + " waits for the workspace"
	}
	if task != "" && task != "blocked" {
		return who + ": " + task
	}
	return who + " is blocked"
}

// deriveFlow exports the internal bookkeeping as the fields the screen reads.
func (s *State) deriveFlow() {
	s.Attention = Attention{}
	if n := len(s.attn); n > 0 {
		s.Attention = s.attn[n-1].Attention
	}
	s.WaitingOn = map[string]string{}
	for _, r := range s.attn {
		if r.Kind == "peer" && r.Actor != "" {
			s.WaitingOn[r.Actor] = str(r.Ref, "peer")
		}
	}
	s.StageRun = make([]StageProgress, len(s.stageRun))
	for i, st := range s.stageRun {
		st.Submitted = append([]string{}, st.Submitted...)
		s.StageRun[i] = st
	}
}

// noteStageEntered records that the run entered a stage. Entering a stage the
// log already shows as the latest one (stage.advanced is followed by
// stage.entered for the same stage) adds nothing; a stage entered again after
// leaving it gets its own record, because its submissions start from zero.
func (s *State) noteStageEntered(name string, index int) {
	if n := len(s.stageRun); n > 0 && s.stageRun[n-1].Name == name && !s.stageRun[n-1].Left {
		s.stageRun[n-1].Index = index
		s.stageRun[n-1].Submitted = nil
		return
	}
	s.stageRun = append(s.stageRun, StageProgress{Name: name, Index: index})
}

// noteSubmitted adds a submitter to the current stage.
func (s *State) noteSubmitted(actor string) {
	n := len(s.stageRun)
	if n == 0 {
		return
	}
	s.stageRun[n-1].Submitted = append(s.stageRun[n-1].Submitted, actor)
}

// noteStageLeft marks the named stage as left.
func (s *State) noteStageLeft(name string) {
	for i := len(s.stageRun) - 1; i >= 0; i-- {
		if s.stageRun[i].Name == name && !s.stageRun[i].Left {
			s.stageRun[i].Left = true
			return
		}
	}
}
