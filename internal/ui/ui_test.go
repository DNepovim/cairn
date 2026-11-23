package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
	"github.com/dominikblaha/cairn/internal/tracker/fake"
)

func testModel(t *testing.T) (Model, *fake.Fake) {
	t.Helper()
	dir := t.TempDir()
	state := `{"active":{"kind":"ticket","id":"681"},"agents":[{"id":"a1","description":"#681 build","status":"running","step":"Edit X.cs"}]}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Tracker.Type = "fake"
	cfg.BridgeDir = dir
	cfg.Workflow.LocalTestPort = 0 // no lsof in unit tests
	fx := fake.New(fake.Fixtures{
		Me:      tracker.User{Username: "me"},
		Items:   []tracker.Item{{ID: "681", Title: "Templates", Labels: []string{"Code review"}}, {ID: "700", Title: "Other"}},
		Details: map[string]tracker.Detail{"681": {Item: tracker.Item{ID: "681", Title: "Templates"}, Description: "See #700"}},
	})
	m := New(cfg, fx, Options{NoWatch: true, Exec: func(string, ...string) error { return nil }})
	m = step(m, tea.WindowSizeMsg{Width: 60, Height: 40})
	m = run(m, m.fetchBoard())
	return m, fx
}

// step applies one message.
func step(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// run executes a command synchronously and feeds its messages back (one level
// of batching is enough for these tests; ticks are skipped).
func run(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = run(m, c)
		}
		return m
	}
	switch msg.(type) {
	case boardMsg, detailMsg, bridgeMsg, commentSentMsg, imageMsg:
		next, cmd := m.Update(msg)
		return run(next.(Model), cmd)
	}
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		next, cmd := m.Update(key(k))
		m = run(next.(Model), cmd)
	}
	return m
}

func TestBridgeActive_OpensTicketInDetail(t *testing.T) {
	m, _ := testModel(t)
	m = run(m, func() tea.Msg { return bridgeMsg{snap: bridge.Load(m.cfg.BridgeDir)} })
	if m.sel != (selection{selTicket, "681"}) {
		t.Fatalf("sel = %+v", m.sel)
	}
	view := m.View()
	for _, want := range []string{"#681 Templates", "🤖 pracuje subagent", "⎿ Edit X.cs", "Moje tickety (2)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestComment_RequiresConfirmAndSendsThroughTracker(t *testing.T) {
	m, fx := testModel(t)
	m = press(m, "enter") // open the ticket under the cursor (681 sorts first)
	m = press(m, "c")
	if m.mode != modeComment || m.commentFor != "681" {
		t.Fatalf("mode=%v for=%q", m.mode, m.commentFor)
	}
	m = press(m, "Hotovo")
	m = press(m, "ctrl+s")
	if m.mode != modeConfirm || len(fx.Sent) != 0 {
		t.Fatalf("must ask before sending: mode=%v sent=%v", m.mode, fx.Sent)
	}
	m = press(m, "n") // back to editing, nothing sent
	if m.mode != modeComment || len(fx.Sent) != 0 {
		t.Fatalf("n must not send: %v", fx.Sent)
	}
	m = press(m, "ctrl+s", "y")
	if len(fx.Sent) != 1 || fx.Sent[0] != "681: Hotovo" {
		t.Fatalf("sent = %v", fx.Sent)
	}
	if m.mode != modeNormal || m.comment.Value() != "" {
		t.Errorf("draft not cleared: mode=%v value=%q", m.mode, m.comment.Value())
	}
}

func TestFilter_NarrowsLists(t *testing.T) {
	m, _ := testModel(t)
	m = press(m, "/", "o", "t", "h", "enter")
	if es := m.entries(secTickets); len(es) != 1 || es[0].sel.id != "700" {
		t.Fatalf("entries = %+v", es)
	}
	m = press(m, "esc")
	if len(m.entries(secTickets)) != 2 {
		t.Error("esc should clear the filter")
	}
}

func TestSectionKeys_FocusAndCollapse(t *testing.T) {
	m, _ := testModel(t)
	m = press(m, "3")
	if m.focus != secSide {
		t.Fatalf("focus = %v", m.focus)
	}
	m = press(m, "z")
	if !m.collapsed[secSide] {
		t.Error("z should collapse")
	}
	m = press(m, "4") // no 4th section any more
	if m.focus != secSide {
		t.Errorf("4 moved focus to %v", m.focus)
	}
}

func TestWaterfill(t *testing.T) {
	got := waterfill(10, []int{2, 20})
	if got[0] != 2 || got[1] != 8 {
		t.Errorf("got %v", got)
	}
}
