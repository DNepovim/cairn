// Package config loads cairn's per-project configuration.
//
// Lookup order:
//  1. an explicit path (cairn --config FILE)
//  2. .claude/cairn.json in the current directory or any parent
//  3. ~/.config/cairn/config.json
//
// Every field has a default, so a config file only needs the parts that differ.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the whole configuration file.
type Config struct {
	Tracker  Tracker  `json:"tracker"`
	Workflow Workflow `json:"workflow"`
	Refresh  Refresh  `json:"refresh"`
	// BridgeDir is where Claude Code (the mod) writes state.json / side.json.
	BridgeDir string `json:"bridgeDir"`

	// Path is the file the config was loaded from ("" = built-in defaults).
	Path string `json:"-"`
}

// Tracker says which issue tracker adapter to use and where.
type Tracker struct {
	// Type selects the adapter: "gitlab" (implemented), "fake" (fixtures, for tests/demos).
	// Planned: "github", "jira", "notion".
	Type string `json:"type"`
	// Host is the tracker host, e.g. "git.applifting.cz" (gitlab) or "github.com".
	Host string `json:"host"`
	// Project is the project path, e.g. "yetanothercorplifting/corplifting".
	Project string `json:"project"`
	// Fixtures is a JSON file the "fake" adapter reads.
	Fixtures string `json:"fixtures,omitempty"`
}

// Workflow maps tracker labels and CI onto cairn's flow states.
type Workflow struct {
	// BoardLabels are substrings of labels that mark a board column ("Code review", "Ready for"…).
	BoardLabels []string `json:"boardLabels"`
	// DevTestStages are board columns in which a merged & deployed item counts as
	// "ready for a dev test" (✅). In later columns somebody else owns the item.
	DevTestStages []string `json:"devTestStages"`
	// PriorityLabels from the highest to the lowest priority.
	PriorityLabels []string `json:"priorityLabels"`
	// BugLabel sorts bugs before other items of the same priority.
	BugLabel string `json:"bugLabel"`
	// EpicLabelPrefix marks epic labels ("epic-"); shown dimmed.
	EpicLabelPrefix string `json:"epicLabelPrefix"`
	// HiddenLabelPrefixes are never shown as tags ("Environment").
	HiddenLabelPrefixes []string `json:"hiddenLabelPrefixes"`
	// LocalTestPort is where the local approval run listens (🧪).
	LocalTestPort int `json:"localTestPort"`
	// DeployRef is the branch whose pipelines deploy to the dev environment.
	DeployRef string `json:"deployRef"`
	// DeployJob is the CI job that deploys ("deploy-dev"); "" = whole pipeline.
	DeployJob string `json:"deployJob"`
	// RepoPath is a local clone used for "is the merge commit deployed" ancestry checks.
	RepoPath string `json:"repoPath"`
}

// Refresh intervals in seconds.
type Refresh struct {
	BoardSeconds  int `json:"boardSeconds"`
	DetailSeconds int `json:"detailSeconds"`
}

// Board returns the board polling interval.
func (r Refresh) Board() time.Duration { return time.Duration(r.BoardSeconds) * time.Second }

// Detail returns the detail polling interval.
func (r Refresh) Detail() time.Duration { return time.Duration(r.DetailSeconds) * time.Second }

// Default returns the configuration used when no file sets a value.
func Default() Config {
	return Config{
		Tracker: Tracker{Type: "gitlab", Host: "gitlab.com"},
		Workflow: Workflow{
			BoardLabels:         []string{"Waiting on developer", "Developing", "Code review", "Ready for", "Backlog"},
			DevTestStages:       []string{"Waiting on developer", "Code review", "Developing"},
			PriorityLabels:      []string{"Priority: Highest", "Priority: High", "Priority: Average", "Priority: Low", "Priority: Lowest"},
			BugLabel:            "Bug",
			EpicLabelPrefix:     "epic-",
			HiddenLabelPrefixes: []string{"Environment"},
			LocalTestPort:       5013,
			DeployRef:           "develop",
			DeployJob:           "deploy-dev",
		},
		Refresh:   Refresh{BoardSeconds: 30, DetailSeconds: 60},
		BridgeDir: "~/.claude/cairn",
	}
}

// ErrNotFound is returned by Find when no config file exists anywhere.
var ErrNotFound = errors.New("no cairn config found (.claude/cairn.json upward from cwd, or ~/.config/cairn/config.json)")

// Find returns the config file cairn would use, starting the upward search at dir.
func Find(dir, home string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		p := filepath.Join(d, ".claude", "cairn.json")
		if fileExists(p) {
			return p, nil
		}
		if parent := filepath.Dir(d); parent == d {
			break
		}
	}
	if home != "" {
		p := filepath.Join(home, ".config", "cairn", "config.json")
		if fileExists(p) {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// Load reads the config at path on top of the defaults. An empty path searches
// with Find from the current directory.
func Load(path string) (Config, error) {
	home, _ := os.UserHomeDir()
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Default(), err
		}
		path, err = Find(cwd, home)
		if err != nil {
			return Default(), err
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Default(), err
	}
	cfg, err := Parse(raw)
	if err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	cfg.BridgeDir = ExpandHome(cfg.BridgeDir, home)
	cfg.Workflow.RepoPath = ExpandHome(cfg.Workflow.RepoPath, home)
	if cfg.Tracker.Fixtures != "" {
		cfg.Tracker.Fixtures = ExpandHome(cfg.Tracker.Fixtures, home)
		if !filepath.IsAbs(cfg.Tracker.Fixtures) {
			cfg.Tracker.Fixtures = filepath.Join(filepath.Dir(path), cfg.Tracker.Fixtures)
		}
	}
	return cfg, nil
}

// Parse decodes JSON over the defaults (missing fields keep their default) and validates it.
func Parse(raw []byte) (Config, error) {
	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Refresh.BoardSeconds < 5 {
		cfg.Refresh.BoardSeconds = 5
	}
	if cfg.Refresh.DetailSeconds < 5 {
		cfg.Refresh.DetailSeconds = 5
	}
	if cfg.Tracker.Type == "" {
		return cfg, errors.New("tracker.type is required")
	}
	if cfg.Tracker.Type != "fake" && cfg.Tracker.Project == "" {
		return cfg, errors.New("tracker.project is required")
	}
	return cfg, nil
}

// ExpandHome turns a leading "~/" into the home directory.
func ExpandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") && home != "" {
		return filepath.Join(home, p[2:])
	}
	return p
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
