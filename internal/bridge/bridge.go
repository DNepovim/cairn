// Package bridge reads what Claude Code (the ticket-pane mod) shares with
// cairn through files in ~/.claude/cairn/. cairn never writes there.
//
//	state.json  written by the mod: active item, subagents
//	side.json   written by Claude: work without a ticket (B1, B2…)
//
// Every file may be missing, partial or mid-write; Load never fails, it
// reports per-file errors and keeps whatever parsed.
package bridge

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// File names inside the bridge directory.
const (
	StateFile = "state.json"
	SideFile  = "side.json"
)

// Active is the item the chat is about right now.
type Active struct {
	Kind string `json:"kind"` // "ticket" or "side"
	ID   string `json:"id"`
}

// Agent is a Claude Code subagent.
type Agent struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Status      string   `json:"status"` // "running", "completed", "failed", "killed", "idle"…
	Step        string   `json:"step"`   // its current tool call, as shown under the prompt
	Tickets     []string `json:"tickets"`
}

// Done reports whether the agent has finished. Idle/waiting still counts as working.
func (a Agent) Done() bool {
	switch a.Status {
	case "completed", "failed", "killed":
		return true
	}
	return false
}

// State is state.json.
type State struct {
	UpdatedAt time.Time `json:"updatedAt"`
	Active    *Active   `json:"active"`
	Agents    []Agent   `json:"agents"`
	// Unknown fields (e.g. an old "notion" block) are ignored.
}

// SideItem is one entry of side.json.
type SideItem struct {
	ID     string `json:"id"`
	Icon   string `json:"icon"`
	Title  string `json:"title"`
	Status string `json:"status"`
	URL    string `json:"url"`
	Note   string `json:"note"`
}

// Snapshot is everything read from the bridge directory at one moment.
type Snapshot struct {
	State    State
	Side     []SideItem
	StateErr error // nil when state.json is fine or simply missing
	SideErr  error
	Missing  bool // state.json does not exist (mod not installed / not running)
}

// Load reads the bridge directory. Missing files are not errors.
func Load(dir string) Snapshot {
	var s Snapshot
	raw, err := os.ReadFile(filepath.Join(dir, StateFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.Missing = true
	case err != nil:
		s.StateErr = err
	default:
		s.State, s.StateErr = ParseState(raw)
	}

	raw, err = os.ReadFile(filepath.Join(dir, SideFile))
	if err == nil {
		s.Side, s.SideErr = ParseSide(raw)
	} else if !errors.Is(err, fs.ErrNotExist) {
		s.SideErr = err
	}
	return s
}

// ParseState decodes state.json. Unknown fields are ignored; agents without
// an id are dropped.
func ParseState(raw []byte) (State, error) {
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, err
	}
	agents := st.Agents[:0]
	for _, a := range st.Agents {
		if a.ID != "" {
			agents = append(agents, a)
		}
	}
	st.Agents = agents
	if st.Active != nil && st.Active.ID == "" {
		st.Active = nil
	}
	return st, nil
}

// ParseSide decodes side.json, keeping only entries with an id and a title.
func ParseSide(raw []byte) ([]SideItem, error) {
	var items []SideItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := items[:0]
	for _, it := range items {
		if it.ID != "" && it.Title != "" {
			out = append(out, it)
		}
	}
	return out, nil
}

// Watch notifies on changes in dir (created if missing). Bursts of events are
// coalesced into one signal after a short quiet period. Call stop to end it.
func Watch(dir string) (changes <-chan struct{}, stop func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		return nil, nil, err
	}
	out := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		const quiet = 120 * time.Millisecond
		var timer *time.Timer
		fire := make(chan struct{}, 1)
		for {
			select {
			case <-done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Ext(ev.Name) == ".tmp" {
					continue // half-written files of an atomic write
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(quiet, func() {
					select {
					case fire <- struct{}{}:
					default:
					}
				})
			case <-fire:
				select {
				case out <- struct{}{}:
				default: // a signal is already pending
				}
			case <-w.Errors:
				// keep watching; a broken watcher only costs freshness
			}
		}
	}()
	return out, func() { close(done); w.Close() }, nil
}
