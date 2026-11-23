package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
	"github.com/dominikblaha/cairn/internal/tracker/fake"
)

// clock is the tests' fake time (Options.Now).
var clock = time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)

func testModel(t *testing.T) (Model, *fake.Fake) {
	t.Helper()
	dir := t.TempDir()
	state := `{"active":{"kind":"ticket","id":"681"},"agents":[{"id":"a1","description":"#681 build","status":"running","step":"Edit X.cs"}]}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	side := `[{"id":"B1","title":"One","status":"s"},{"id":"B2","title":"Two"}]`
	if err := os.WriteFile(filepath.Join(dir, "side.json"), []byte(side), 0o644); err != nil {
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
	m := New(cfg, fx, Options{NoWatch: true, Exec: func(string, ...string) error { return nil }, Now: func() time.Time { return clock }})
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
	for _, want := range []string{"#681 Templates", "🤖 pracuje subagent", "⎿ Edit X.cs", "Moje tickety (0/2)"} {
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
	m = press(m, "1")
	if m.focus != secDetail {
		t.Fatalf("focus = %v", m.focus)
	}
	m = press(m, "2") // focus the list panel on its current tab
	if m.focus != secTickets || m.tab != secTickets {
		t.Fatalf("focus=%v tab=%v", m.focus, m.tab)
	}
	m = press(m, "2") // again: flip the tab
	if m.focus != secSide || m.tab != secSide {
		t.Fatalf("2 again: focus=%v tab=%v", m.focus, m.tab)
	}
	m = press(m, "[")
	if m.tab != secTickets {
		t.Errorf("[ should switch back, tab=%v", m.tab)
	}
	m = press(m, "z")
	if !m.collapsed[secTickets] || !m.collapsed[secSide] {
		t.Error("z collapses panel [2] for both tabs")
	}
	m = press(m, "z", "]", "j")
	if m.cursor[secSide] != 1 || m.cursor[secTickets] != 0 {
		t.Errorf("cursor per tab: %v", m.cursor)
	}
	m = press(m, "[")
	if m.cursor[secSide] != 1 {
		t.Error("tab switch must keep the other tab's cursor")
	}
}

func TestWaterfill(t *testing.T) {
	got := waterfill(10, []int{2, 20})
	if got[0] != 2 || got[1] != 8 {
		t.Errorf("got %v", got)
	}
}

func TestMultiRuneKeyMsg_RunsEachKey(t *testing.T) {
	m, _ := testModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1z")})
	m = next.(Model)
	if m.focus != secDetail || !m.collapsed[secDetail] {
		t.Errorf("focus=%v collapsed=%v", m.focus, m.collapsed)
	}
}

func TestBridgeActive_DoesNotMoveCursorWhileNavigating(t *testing.T) {
	m, _ := testModel(t)
	m = press(m, "j")                    // user moves to #700
	snap := bridge.Load(m.cfg.BridgeDir) // active = #681
	m = step(m, bridgeMsg{snap: snap})
	if m.sel.id != "681" {
		t.Fatalf("Detail should follow the chat, sel = %+v", m.sel)
	}
	if e, _ := m.current(); e.sel.id != "700" {
		t.Errorf("cursor moved to %q while navigating", e.sel.id)
	}

	// After the grace period the cursor follows again.
	m.navAt = clock.Add(-navGrace - time.Second)
	m.lastActive = ""
	m = step(m, bridgeMsg{snap: snap})
	if e, _ := m.current(); e.sel.id != "681" {
		t.Errorf("cursor should follow after grace, at %q", e.sel.id)
	}
}

func TestShortURL(t *testing.T) {
	got := ShortURL("https://claude.ai/design/p/56727f30-596d-4aac-be32-5d1f43b83d86?file=x", 30)
	if lipgloss.Width(got) != 30 || !strings.HasPrefix(got, "claude.ai/design/") || !strings.Contains(got, "…") {
		t.Errorf("got %q (%d)", got, lipgloss.Width(got))
	}
}

func TestLongURL_ShortenedAndOpenable(t *testing.T) {
	m, _ := testModel(t)
	long := "https://example.com/" + strings.Repeat("segment/", 12) + "end?x=1&y=2"
	out := m.shortenURLs("see "+long+" now", 40)
	if strings.Contains(out, long) || !strings.Contains(out, "↗1") || m.cache.links[1] != long {
		t.Errorf("out = %q links = %v", out, m.cache.links)
	}
	short := "https://example.com/a"
	if m.shortenURLs(short, 40) != short {
		t.Error("short URLs stay")
	}
}

func TestView_NeverOverflows(t *testing.T) {
	for _, w := range []int{45, 52, 60, 80} {
		m, _ := testModel(t)
		m = step(m, tea.WindowSizeMsg{Width: w, Height: 45})
		m = run(m, func() tea.Msg { return bridgeMsg{snap: bridge.Load(m.cfg.BridgeDir)} })
		for _, keys := range [][]string{{}, {"2", "z"}, {"]"}, {"?"}} {
			mm := press(m, keys...)
			lines := strings.Split(mm.View(), "\n")
			if len(lines) != 45 {
				t.Errorf("w=%d keys=%v: %d lines", w, keys, len(lines))
			}
			for i, l := range lines {
				if lipgloss.Width(l) > w {
					t.Errorf("w=%d keys=%v line %d is %d wide: %q", w, keys, i, lipgloss.Width(l), l)
				}
			}
		}
	}
}

func TestTabs_TitleAndBridgeSide(t *testing.T) {
	m, _ := testModel(t)
	title, _ := m.sectionTitle(secTickets, 5)
	if plain := ansiStrip(title); plain != "[2] Moje tickety (0/2) — Bez ticketu (0/2)" {
		t.Errorf("title = %q", plain)
	}
	if s, ok := m.tabAt(2 + len("[2] ")); !ok || s != secTickets {
		t.Errorf("click on first tab = %v %v", s, ok)
	}
	if s, ok := m.tabAt(2 + len("[2] ") + lipgloss.Width("Moje tickety (0/2)") + 3); !ok || s != secSide {
		t.Errorf("click on second tab = %v %v", s, ok)
	}
	snap := bridge.Load(m.cfg.BridgeDir)
	snap.State.Active = &bridge.Active{Kind: "side", ID: "B1"}
	m = step(m, bridgeMsg{snap: snap})
	if m.tab != secSide || m.sel != (selection{selSide, "B1"}) {
		t.Errorf("side active: tab=%v sel=%+v", m.tab, m.sel)
	}
}

func ansiStrip(s string) string { return ansi.Strip(s) }

func TestNeedsMe_MarksRowsAndTitle(t *testing.T) {
	m, _ := testModel(t)
	acts := `[{"id":"#700","icon":"🧪","action":"otestuj lokálně"},{"id":"B2","icon":"🔀","action":"merge !309"}]`
	if err := os.WriteFile(filepath.Join(m.cfg.BridgeDir, "actions.json"), []byte(acts), 0o644); err != nil {
		t.Fatal(err)
	}
	m.opts.Now = time.Now // the file's mtime is real time
	m = step(m, bridgeMsg{snap: bridge.Load(m.cfg.BridgeDir)})
	title, _ := m.sectionTitle(secTickets, 5)
	if plain := ansiStrip(title); plain != "[2] Moje tickety (1/2) — Bez ticketu (1/2)" {
		t.Errorf("title = %q", plain)
	}
	view := ansiStrip(m.View())
	if !strings.Contains(view, "● #700 Other") || !strings.Contains(view, "👉 🧪 otestuj lokálně") {
		t.Errorf("row not marked:\n%s", view)
	}
	m = press(m, "]")
	if !strings.Contains(ansiStrip(m.View()), "👉 🔀 merge !309") {
		t.Error("side action line missing")
	}
}
