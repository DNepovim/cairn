package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_PartialFile_KeepsDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`{"tracker":{"type":"gitlab","host":"git.example.com","project":"a/b"},"workflow":{"localTestPort":6000}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tracker.Host != "git.example.com" || cfg.Tracker.Project != "a/b" {
		t.Errorf("tracker not read: %+v", cfg.Tracker)
	}
	if cfg.Workflow.LocalTestPort != 6000 {
		t.Errorf("port = %d, want 6000", cfg.Workflow.LocalTestPort)
	}
	if cfg.Workflow.DeployRef != "develop" || len(cfg.Workflow.PriorityLabels) != 5 {
		t.Errorf("defaults lost: %+v", cfg.Workflow)
	}
	if cfg.Refresh.BoardSeconds != 30 {
		t.Errorf("refresh default lost: %+v", cfg.Refresh)
	}
}

func TestParse_MissingProject_Errors(t *testing.T) {
	if _, err := Parse([]byte(`{"tracker":{"type":"gitlab"}}`)); err == nil {
		t.Fatal("want error for missing project")
	}
}

func TestParse_InvalidJSON_Errors(t *testing.T) {
	if _, err := Parse([]byte(`{`)); err == nil {
		t.Fatal("want error")
	}
}

func TestParse_TooFastRefresh_Clamped(t *testing.T) {
	cfg, err := Parse([]byte(`{"tracker":{"type":"fake"},"refresh":{"boardSeconds":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.BoardSeconds != 5 {
		t.Errorf("board = %d, want 5", cfg.Refresh.BoardSeconds)
	}
}

func TestFind_SearchesUpwardThenHome(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	must(t, os.MkdirAll(deep, 0o755))

	if _, err := Find(deep, home); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	homeCfg := filepath.Join(home, ".config", "cairn", "config.json")
	must(t, os.MkdirAll(filepath.Dir(homeCfg), 0o755))
	must(t, os.WriteFile(homeCfg, []byte(`{}`), 0o644))
	if p, _ := Find(deep, home); p != homeCfg {
		t.Errorf("fallback: got %q", p)
	}

	projCfg := filepath.Join(root, "a", ".claude", "cairn.json")
	must(t, os.MkdirAll(filepath.Dir(projCfg), 0o755))
	must(t, os.WriteFile(projCfg, []byte(`{}`), 0o644))
	if p, _ := Find(deep, home); p != projCfg {
		t.Errorf("project: got %q", p)
	}
}

func TestLoad_ExpandsHomeAndRelativeFixtures(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	must(t, os.WriteFile(p, []byte(`{"tracker":{"type":"fake","fixtures":"fx.json"},"bridgeDir":"~/x"}`), 0o644))
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tracker.Fixtures != filepath.Join(dir, "fx.json") {
		t.Errorf("fixtures = %q", cfg.Tracker.Fixtures)
	}
	home, _ := os.UserHomeDir()
	if cfg.BridgeDir != filepath.Join(home, "x") {
		t.Errorf("bridgeDir = %q", cfg.BridgeDir)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
