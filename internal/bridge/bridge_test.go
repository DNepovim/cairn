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
	 "notion":{"fetchedAt":"14:05","error":null,"items":[{"url":"u","title":"T","status":"Submitted","assigned":false,"others":0,"changed":true}]}}`)
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
	if len(s.State.Notion.Items) != 1 || !s.State.Notion.Items[0].Changed {
		t.Errorf("notion = %+v", s.State.Notion)
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

func TestMarkNotionRead_RoundTripAndLocalOverride(t *testing.T) {
	dir := t.TempDir()
	updated := time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)
	write(t, dir, StateFile, `{"updatedAt":"2026-11-23T14:00:00Z","notion":{"items":[{"url":"u","changed":true}]}}`)
	s := Load(dir)
	if !NotionChanged(s, s.State.Notion.Items[0]) {
		t.Fatal("should be unread before marking")
	}
	if err := MarkNotionRead(dir, updated.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s = Load(dir)
	if s.ReadAt.IsZero() {
		t.Fatal("readAt not read back")
	}
	if NotionChanged(s, s.State.Notion.Items[0]) {
		t.Error("should be read after marking")
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
