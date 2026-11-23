// Package workflow computes where each of my items stands in our own flow
// (subagent → local test → MR → deploy → dev test), on top of the tracker's
// board labels. It is pure logic: inputs are gathered elsewhere, so every
// rule here is unit-tested.
package workflow

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
)

// Kind is the flow state, in the order the rules are checked.
type Kind int

const (
	Idle      Kind = iota // ⏸ nothing happening, board column shown
	Agent                 // 🤖 a subagent works on it
	LocalTest             // 🧪 served on the local test port
	ReviewMR              // 📝 an MR is open
	Deploying             // 🚀 merged, deploy to dev running
	DevTest               // ✅ merged & deployed, ready for a dev test
)

// Flow is the computed state of one item.
type Flow struct {
	Kind   Kind
	Text   string // exactly the mod's wording, e.g. "📝 MR !512 otevřený ✗ pipeline"
	Active bool   // something is happening on it right now (sorts first)
	Failed bool   // open MR with a failed pipeline
}

// Input is everything the flow rules look at for one item.
type Input struct {
	ID           string
	Labels       []string
	AgentRunning bool   // a running subagent is attached to this item
	Served       string // command line of the process on the local test port ("" = none)
	Changes      []tracker.Change
	// NewestMergedOnDev: the newest merged change's commit is an ancestor of
	// the deployed revision. Only meaningful when a merged change exists.
	NewestMergedOnDev bool
	DeployInProgress  bool
}

// Stage returns the item's board column label ("" = none).
func Stage(labels []string, wf config.Workflow) string {
	for _, l := range labels {
		if containsAny(l, wf.BoardLabels) {
			return l
		}
	}
	return ""
}

// NewestMerged returns the most recently merged change that has a commit.
func NewestMerged(changes []tracker.Change) *tracker.Change {
	var best *tracker.Change
	for i := range changes {
		c := &changes[i]
		if c.State != "merged" || c.MergeCommit == "" {
			continue
		}
		if best == nil || c.MergedAt.After(best.MergedAt) {
			best = c
		}
	}
	return best
}

// Compute applies the rules in the mod's order; the first that holds wins.
func Compute(in Input, wf config.Workflow) Flow {
	stage := Stage(in.Labels, wf)
	merged := NewestMerged(in.Changes)
	var open *tracker.Change
	for i := range in.Changes {
		if in.Changes[i].State == "opened" {
			open = &in.Changes[i]
			break
		}
	}
	switch {
	case in.AgentRunning:
		return Flow{Kind: Agent, Text: "🤖 pracuje subagent", Active: true}
	case in.Served != "" && ServedBy(in.Served, in.ID):
		return Flow{Kind: LocalTest, Text: "🧪 připraveno k lokálnímu testu", Active: true}
	case open != nil:
		f := Flow{Kind: ReviewMR, Text: "📝 MR !" + open.ID + " otevřený", Active: true}
		if open.Pipeline == "failed" {
			f.Text += " ✗ pipeline"
			f.Failed = true
		}
		return f
	case merged != nil && !in.NewestMergedOnDev && in.DeployInProgress:
		return Flow{Kind: Deploying, Text: "🚀 nasazuje se na dev", Active: true}
	case merged != nil && in.NewestMergedOnDev && containsAny(stage, wf.DevTestStages):
		return Flow{Kind: DevTest, Text: "✅ připraveno k testu na devu", Active: true}
	case stage != "":
		return Flow{Kind: Idle, Text: "⏸ " + stage}
	default:
		return Flow{Kind: Idle, Text: "—"}
	}
}

// ServedBy reports whether a server command line belongs to item id: the
// worktree path contains "wt-681…" or "/681…" (not "/6810").
func ServedBy(command, id string) bool {
	re := regexp.MustCompile(`(wt-|/)` + regexp.QuoteMeta(id) + `[^0-9]`)
	return re.MatchString(command + " ")
}

// Priority is the index in wf.PriorityLabels (0 = highest), or len for none.
func Priority(labels []string, wf config.Workflow) int {
	for i, p := range wf.PriorityLabels {
		for _, l := range labels {
			if l == p {
				return i
			}
		}
	}
	return len(wf.PriorityLabels)
}

// Rank is the sort key: active first, then priority, bugs before the rest.
func Rank(f Flow, labels []string, wf config.Workflow) int {
	r := Priority(labels, wf) * 10
	if !f.Active {
		r += 100
	}
	if !hasLabel(labels, wf.BugLabel) {
		r++
	}
	return r
}

// Row is one line of "Moje tickety".
type Row struct {
	Item   tracker.Item
	Flow   Flow
	Rank   int
	Agents []bridge.Agent // subagents attached to it (not killed), newest last
}

// Sort orders rows by rank, then newer (higher) ids first.
func Sort(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Rank != rows[j].Rank {
			return rows[i].Rank < rows[j].Rank
		}
		return idLess(rows[j].Item.ID, rows[i].Item.ID)
	})
}

// idLess compares numerically when both ids are numbers.
func idLess(a, b string) bool {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	if ea == nil && eb == nil {
		return na < nb
	}
	return a < b
}

// Tags are the labels worth showing next to an item: no board column, no
// hidden prefixes; priority becomes "⚑ High".
func Tags(labels []string, wf config.Workflow) []string {
	var out []string
	for _, l := range labels {
		if containsAny(l, wf.BoardLabels) || hasPrefixAny(l, wf.HiddenLabelPrefixes) {
			continue
		}
		if Priority([]string{l}, wf) < len(wf.PriorityLabels) {
			_, short, ok := strings.Cut(l, ": ")
			if !ok {
				short = l
			}
			l = "⚑ " + short
		}
		out = append(out, l)
	}
	return out
}

// AgentsByItem attaches subagents to items: by the tickets the mod recorded
// from the brief, or by a "#681 …" prefix of the agent's name. Killed agents
// are dropped; the rest are kept (finished ones shown dimmed).
func AgentsByItem(agents []bridge.Agent, pattern *regexp.Regexp) (byItem map[string][]bridge.Agent, unattached []bridge.Agent) {
	byItem = map[string][]bridge.Agent{}
	for _, a := range agents {
		ids := map[string]bool{}
		var order []string
		for _, id := range append(append([]string{}, a.Tickets...), tracker.TicketsInName(a.Description, pattern)...) {
			if !ids[id] {
				ids[id] = true
				order = append(order, id)
			}
		}
		if len(order) == 0 {
			if !a.Done() {
				unattached = append(unattached, a)
			}
			continue
		}
		if a.Status == "killed" {
			continue
		}
		for _, id := range order {
			byItem[id] = append(byItem[id], a)
		}
	}
	for id, list := range byItem {
		if len(list) > 6 {
			byItem[id] = list[len(list)-6:]
		}
	}
	return byItem, unattached
}

// AnyRunning reports whether any agent in the list still works.
func AnyRunning(agents []bridge.Agent) bool {
	for _, a := range agents {
		if !a.Done() {
			return true
		}
	}
	return false
}

// LocalServer returns the command line of the process listening on port, or "".
func LocalServer(ctx context.Context, port int) string {
	if port <= 0 {
		return ""
	}
	pid, err := exec.CommandContext(ctx, "lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(pid)), "\n")
	if first == "" {
		return ""
	}
	cmd, err := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", first).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(cmd))
}

// IsAncestor runs `git merge-base --is-ancestor commit of` in repo.
func IsAncestor(ctx context.Context, repo, commit, of string) bool {
	if repo == "" || commit == "" || of == "" {
		return false
	}
	return exec.CommandContext(ctx, "git", "-C", repo, "merge-base", "--is-ancestor", commit, of).Run() == nil
}

func containsAny(s string, subs []string) bool {
	if s == "" {
		return false
	}
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func hasPrefixAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
