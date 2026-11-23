package workflow

import (
	"strings"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
)

// Needs says which items wait on me, by ticket id and side id. An item is in
// the map when it needs me; its slice holds the /actions entries for it
// (empty when the reason is a heuristic).
type Needs struct {
	Tickets map[string][]bridge.Action
	Side    map[string][]bridge.Action
}

// NeedsInput is what NeedsMe looks at.
type NeedsInput struct {
	Rows    []Row
	Changes map[string][]tracker.Change // per ticket id, to resolve "!313"
	Side    []bridge.SideItem
	Actions []bridge.Action
	// UseActions: actions.json exists and is fresh; otherwise heuristics.
	UseActions bool
	Ref        func(id string) string // "681" → "#681"
}

// NeedsMe resolves what waits on me: from /actions when fresh, otherwise
// from the flow (🧪, ✅, failed MR pipeline), needsMeLabels and side items'
// needsYou flag.
func NeedsMe(in NeedsInput, wf config.Workflow) Needs {
	n := Needs{Tickets: map[string][]bridge.Action{}, Side: map[string][]bridge.Action{}}
	if in.UseActions {
		for _, a := range in.Actions {
			id := strings.TrimSpace(a.ID)
			if t, ok := ticketFor(id, in); ok {
				n.Tickets[t] = append(n.Tickets[t], a)
				continue
			}
			for _, s := range in.Side {
				if s.ID == id {
					n.Side[id] = append(n.Side[id], a)
				}
			}
		}
		return n
	}
	for _, r := range in.Rows {
		if r.Flow.Kind == LocalTest || r.Flow.Kind == DevTest || (r.Flow.Kind == ReviewMR && r.Flow.Failed) {
			n.Tickets[r.Item.ID] = nil
			continue
		}
		for _, l := range r.Item.Labels {
			if containsAny(l, wf.NeedsMeLabels) {
				n.Tickets[r.Item.ID] = nil
			}
		}
	}
	for _, s := range in.Side {
		if s.NeedsYou {
			n.Side[s.ID] = nil
		}
	}
	return n
}

// ticketFor maps an action id to one of my tickets: "#688", "688", or "!313"
// (the ticket whose open MR is !313).
func ticketFor(id string, in NeedsInput) (string, bool) {
	if mr, ok := strings.CutPrefix(id, "!"); ok {
		for _, r := range in.Rows {
			for _, c := range in.Changes[r.Item.ID] {
				if c.ID == mr && c.State == "opened" {
					return r.Item.ID, true
				}
			}
		}
		return "", false
	}
	for _, r := range in.Rows {
		if id == r.Item.ID || (in.Ref != nil && id == in.Ref(r.Item.ID)) {
			return r.Item.ID, true
		}
	}
	return "", false
}
