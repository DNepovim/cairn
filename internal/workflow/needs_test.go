package workflow

import (
	"testing"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/tracker"
)

func needsFixture(useActions bool) NeedsInput {
	return NeedsInput{
		Rows: []Row{
			{Item: tracker.Item{ID: "688"}, Flow: Flow{Kind: LocalTest}},
			{Item: tracker.Item{ID: "650"}, Flow: Flow{Kind: ReviewMR}},
			{Item: tracker.Item{ID: "651"}, Flow: Flow{Kind: ReviewMR, Failed: true}},
			{Item: tracker.Item{ID: "700", Labels: []string{"Waiting on developer"}}, Flow: Flow{Kind: Idle}},
			{Item: tracker.Item{ID: "701", Labels: []string{"Backlog"}}, Flow: Flow{Kind: Idle}},
		},
		Changes: map[string][]tracker.Change{"650": {{ID: "313", State: "opened"}}},
		Side:    []bridge.SideItem{{ID: "B1", NeedsYou: true}, {ID: "B3"}},
		Actions: []bridge.Action{
			{ID: "#688", Icon: "🧪", Action: "lokálně"},
			{ID: "B3", Icon: "🔀", Action: "merge !309"},
			{ID: "!313", Icon: "🔀", Action: "merge?"},
			{ID: "!999", Action: "unknown MR"},
		},
		UseActions: useActions,
		Ref:        func(id string) string { return "#" + id },
	}
}

func TestNeedsMe_FromActions(t *testing.T) {
	n := NeedsMe(needsFixture(true), wf)
	if len(n.Tickets) != 2 || n.Tickets["688"][0].Action != "lokálně" || n.Tickets["650"][0].Action != "merge?" {
		t.Errorf("tickets = %+v", n.Tickets)
	}
	if len(n.Side) != 1 || n.Side["B3"][0].Icon != "🔀" {
		t.Errorf("side = %+v", n.Side)
	}
}

func TestNeedsMe_FallbackHeuristics(t *testing.T) {
	n := NeedsMe(needsFixture(false), wf)
	for _, id := range []string{"688", "651", "700"} {
		if _, ok := n.Tickets[id]; !ok {
			t.Errorf("%s should need me", id)
		}
	}
	for _, id := range []string{"650", "701"} {
		if _, ok := n.Tickets[id]; ok {
			t.Errorf("%s should not need me", id)
		}
	}
	if _, ok := n.Side["B1"]; !ok || len(n.Side) != 1 {
		t.Errorf("side = %+v", n.Side)
	}
}
