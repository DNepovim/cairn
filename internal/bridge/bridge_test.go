package bridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_EmptyDir_DegradesGracefully(t *testing.T) {
	s := Load(t.TempDir())
	if !s.Missing || s.StateErr != nil || s.SideErr != nil {
		t.Errorf("snapshot = %+v", s)
	}
	if s.State.Active != nil || len(s.Side) != 0 {
		t.Errorf("expected empty state: %+v", s)
	}
}

func TestLoad_FullFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, StateFile, `{"updatedAt":"2026-11-23T14:00:00Z",
	 "active":{"kind":"ticket","id":"681"},
	 "agents":[{"id":"a1","description":"#688 review","status":"running","step":"Read X","tickets":["688"]},{"description":"no id"}],
	 "notion":{"items":[{"url":"legacy, ignored"}]}}`)
	write(t, dir, SideFile, `[{"id":"B1","icon":"📝","title":"Notes","status":"draft"},{"id":"","title":"dropped"}]`)
	s := Load(dir)
	if s.StateErr != nil || s.SideErr != nil {
		t.Fatalf("errors: %v %v", s.StateErr, s.SideErr)
	}
	if s.State.Active == nil || s.State.Active.ID != "681" || s.State.Active.Kind != "ticket" {
		t.Errorf("active = %+v", s.State.Active)
	}
	if len(s.State.Agents) != 1 || s.State.Agents[0].Tickets[0] != "688" {
		t.Errorf("agents = %+v", s.State.Agents)
	}
	if len(s.Side) != 1 || s.Side[0].ID != "B1" {
		t.Errorf("side = %+v", s.Side)
	}
}

func TestLoad_PartialAndBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, StateFile, `{"active":null}`)
	write(t, dir, SideFile, `[{"id":"B1",`) // mid-write
	s := Load(dir)
	if s.StateErr != nil || s.State.Active != nil || s.Missing {
		t.Errorf("state: %+v", s)
	}
	if s.SideErr == nil {
		t.Error("want side error for truncated JSON")
	}
}

func TestParseState_EmptyActiveIDIsNil(t *testing.T) {
	st, err := ParseState([]byte(`{"active":{"kind":"ticket","id":""}}`))
	if err != nil || st.Active != nil {
		t.Errorf("st = %+v err = %v", st, err)
	}
}

func TestAgentDone(t *testing.T) {
	for status, want := range map[string]bool{"running": false, "idle": false, "completed": true, "failed": true, "killed": true} {
		if got := (Agent{Status: status}).Done(); got != want {
			t.Errorf("%s: got %v", status, got)
		}
	}
}

func TestWatch_SignalsOnWrite(t *testing.T) {
	dir := t.TempDir()
	ch, stop, err := Watch(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	write(t, dir, StateFile, `{}`)
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no signal")
	}
}

func TestLoad_Actions_FreshAndStale(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if Load(dir).ActionsFresh(now) {
		t.Fatal("no file is not fresh")
	}
	write(t, dir, ActionsFile, `[{"id":"#688","icon":"🧪","action":"test"},{"id":"","action":"dropped"}]`)
	s := Load(dir)
	if s.ActionsErr != nil || len(s.Actions) != 1 || s.Actions[0].ID != "#688" || !s.ActionsFresh(now) {
		t.Fatalf("snapshot = %+v", s)
	}
	old := now.Add(-25 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, ActionsFile), old, old); err != nil {
		t.Fatal(err)
	}
	if Load(dir).ActionsFresh(now) {
		t.Error("25 h old actions.json must be stale")
	}
	write(t, dir, ActionsFile, `[{`)
	if s := Load(dir); s.ActionsErr == nil || s.ActionsFresh(now) {
		t.Error("broken actions.json: error, not fresh")
	}
}

func TestParseSide_NeedsYou(t *testing.T) {
	items, err := ParseSide([]byte(`[{"id":"B1","title":"x","needsYou":true}]`))
	if err != nil || !items[0].NeedsYou {
		t.Errorf("items = %+v err = %v", items, err)
	}
}
