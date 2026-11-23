package workflow

import (
	"fmt"
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

// InboxKind says what an inbox row points at.
type InboxKind int

const (
	InboxTicket InboxKind = iota // an item of the tracker (assigned to me or not)
	InboxSide                    // a side item (B1…)
	InboxChange                  // a lone MR/PR not linked to any of my tickets
)

// InboxItem is one row of the Inbox tab.
type InboxItem struct {
	Kind    InboxKind
	ID      string // ticket id ("494"), side id ("B3") or change id ("313")
	Icon    string
	Actions []bridge.Action // what to do; empty for heuristic rows
	Mine    bool            // a ticket from my assigned list
}

// Inbox lists everything waiting on me. With a fresh actions.json it follows
// that file's order (it is already prioritised) and includes tickets and
// MRs that are not assigned to me; otherwise it is my tickets that need me
// in board order, then side items.
func Inbox(in NeedsInput, wf config.Workflow) []InboxItem {
	var out []InboxItem
	index := map[string]int{} // "kind:id" -> position, to merge repeated targets
	add := func(it InboxItem, a *bridge.Action) {
		key := fmt.Sprint(it.Kind) + ":" + it.ID
		if i, ok := index[key]; ok {
			if a != nil {
				out[i].Actions = append(out[i].Actions, *a)
			}
			return
		}
		if a != nil {
			it.Actions = []bridge.Action{*a}
			if it.Icon == "" {
				it.Icon = a.Icon
			}
		}
		index[key] = len(out)
		out = append(out, it)
	}
	if in.UseActions {
		for i := range in.Actions {
			a := &in.Actions[i]
			id := strings.TrimSpace(a.ID)
			if t, ok := ticketFor(id, in); ok {
				add(InboxItem{Kind: InboxTicket, ID: t, Mine: true}, a)
				continue
			}
			if isSide(id, in.Side) {
				add(InboxItem{Kind: InboxSide, ID: id}, a)
				continue
			}
			if mr, ok := strings.CutPrefix(id, "!"); ok && mr != "" {
				add(InboxItem{Kind: InboxChange, ID: mr}, a)
				continue
			}
			if t := strings.TrimPrefix(id, "#"); t != "" && isDigits(t) {
				add(InboxItem{Kind: InboxTicket, ID: t}, a) // not assigned to me
			}
		}
		return out
	}
	needs := NeedsMe(in, wf)
	for _, r := range in.Rows {
		if _, ok := needs.Tickets[r.Item.ID]; ok {
			add(InboxItem{Kind: InboxTicket, ID: r.Item.ID, Mine: true, Icon: firstWord(r.Flow.Text)}, nil)
		}
	}
	for _, s := range in.Side {
		if _, ok := needs.Side[s.ID]; ok {
			add(InboxItem{Kind: InboxSide, ID: s.ID, Icon: s.Icon}, nil)
		}
	}
	return out
}

func isSide(id string, side []bridge.SideItem) bool {
	for _, s := range side {
		if s.ID == id {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func firstWord(s string) string {
	w, _, _ := strings.Cut(s, " ")
	return w
}
