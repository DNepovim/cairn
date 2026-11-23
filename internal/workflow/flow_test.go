package workflow

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
)

var wf = config.Default().Workflow

var (
	openMR     = tracker.Change{ID: "512", State: "opened", Pipeline: "success"}
	failedMR   = tracker.Change{ID: "513", State: "opened", Pipeline: "failed"}
	mergedMR   = tracker.Change{ID: "500", State: "merged", MergeCommit: "abc", MergedAt: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}
	noCommitMR = tracker.Change{ID: "499", State: "merged"}
	codeReview = []string{"Code review", "Bug"}
	readyForQA = []string{"Ready for testing"}
	unlabelled = []string{}
)

func TestCompute_RulesInOrder(t *testing.T) {
	cases := []struct {
		name   string
		in     Input
		kind   Kind
		text   string
		active bool
	}{
		{"agent wins over everything", Input{ID: "681", AgentRunning: true, Changes: []tracker.Change{openMR}}, Agent, "🤖 pracuje subagent", true},
		{"served from worktree", Input{ID: "681", Served: "dotnet run --project /wt/wt-681-fix/CorpLifting.Web"}, LocalTest, "🧪 připraveno k lokálnímu testu", true},
		{"served by other ticket", Input{ID: "681", Served: "/wt/wt-6810/x", Labels: codeReview}, Idle, "⏸ Code review", false},
		{"open MR", Input{ID: "681", Changes: []tracker.Change{openMR}}, ReviewMR, "📝 MR !512 otevřený", true},
		{"open MR failed pipeline", Input{ID: "681", Changes: []tracker.Change{failedMR}}, ReviewMR, "📝 MR !513 otevřený ✗ pipeline", true},
		{"merged, deploying", Input{ID: "681", Changes: []tracker.Change{mergedMR}, DeployInProgress: true}, Deploying, "🚀 nasazuje se na dev", true},
		{"merged, on dev, code review", Input{ID: "681", Labels: codeReview, Changes: []tracker.Change{mergedMR}, NewestMergedOnDev: true}, DevTest, "✅ připraveno k testu na devu", true},
		{"merged, on dev, but QA owns it", Input{ID: "681", Labels: readyForQA, Changes: []tracker.Change{mergedMR}, NewestMergedOnDev: true}, Idle, "⏸ Ready for testing", false},
		{"merged without commit is ignored", Input{ID: "681", Labels: codeReview, Changes: []tracker.Change{noCommitMR}, DeployInProgress: true}, Idle, "⏸ Code review", false},
		{"merged, not deployed, no deploy", Input{ID: "681", Labels: codeReview, Changes: []tracker.Change{mergedMR}}, Idle, "⏸ Code review", false},
		{"nothing at all", Input{ID: "681", Labels: unlabelled}, Idle, "—", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Compute(c.in, wf)
			if f.Kind != c.kind || f.Text != c.text || f.Active != c.active {
				t.Errorf("got %+v, want kind=%d text=%q active=%v", f, c.kind, c.text, c.active)
			}
		})
	}
}

func TestNewestMerged_PicksLatest(t *testing.T) {
	later := mergedMR
	later.ID, later.MergeCommit, later.MergedAt = "501", "def", mergedMR.MergedAt.Add(time.Hour)
	if got := NewestMerged([]tracker.Change{mergedMR, later, openMR}); got == nil || got.ID != "501" {
		t.Errorf("got %+v", got)
	}
	if NewestMerged([]tracker.Change{openMR, noCommitMR}) != nil {
		t.Error("want nil")
	}
}

func TestServedBy(t *testing.T) {
	if !ServedBy("/Users/x/wt-681/bin/Web", "681") || !ServedBy("/repos/681-fix/Web", "681") {
		t.Error("should match")
	}
	if ServedBy("/repos/6810/Web", "681") || ServedBy("port681x", "681") {
		t.Error("should not match")
	}
}

func TestSort_ActiveThenPriorityThenBugThenNewest(t *testing.T) {
	mk := func(id string, active bool, labels ...string) Row {
		f := Flow{Active: active}
		return Row{Item: tracker.Item{ID: id, Labels: labels}, Flow: f, Rank: Rank(f, labels, wf)}
	}
	rows := []Row{
		mk("600", false, "Priority: Highest", "Bug"),
		mk("601", true, "Priority: Low"),
		mk("602", true, "Priority: High"),
		mk("603", true, "Priority: High", "Bug"),
		mk("604", true, "Priority: High"),
		mk("605", false),
	}
	Sort(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.Item.ID)
	}
	want := "603,604,602,601,600,605"
	if strings.Join(got, ",") != want {
		t.Errorf("order = %s, want %s", strings.Join(got, ","), want)
	}
}

func TestTags_HidesBoardAndEnvironment(t *testing.T) {
	got := Tags([]string{"Code review", "Environment: Dev", "Priority: High", "Bug", "epic-invoices"}, wf)
	if strings.Join(got, "|") != "⚑ High|Bug|epic-invoices" {
		t.Errorf("tags = %v", got)
	}
}

func TestAgentsByItem(t *testing.T) {
	pattern := regexp.MustCompile(`(?:^|[^\w/!\[&])#(\d{3,5})\b`)
	agents := []bridge.Agent{
		{ID: "a1", Description: "#688 review", Status: "running"},
		{ID: "a2", Description: "spec writer", Status: "running", Tickets: []string{"681"}},
		{ID: "a3", Description: "#681 old", Status: "killed"},
		{ID: "a4", Description: "#681 done", Status: "completed"},
		{ID: "a5", Description: "tidy worktrees", Status: "running"},
		{ID: "a6", Description: "finished side job", Status: "completed"},
	}
	by, side := AgentsByItem(agents, pattern)
	if len(by["688"]) != 1 || len(by["681"]) != 2 {
		t.Errorf("by = %+v", by)
	}
	if len(side) != 1 || side[0].ID != "a5" {
		t.Errorf("side = %+v", side)
	}
	if !AnyRunning(by["681"]) {
		t.Error("681 has a running agent")
	}
}
