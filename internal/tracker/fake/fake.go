// Package fake is an in-memory tracker read from a JSON fixture file. It backs
// tests, demos and offline work; AddComment never leaves the process (it is
// appended in memory and logged to <fixtures>.sent.log).
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/dominikblaha/cairn/internal/tracker"
)

// Fixtures is the JSON file layout.
type Fixtures struct {
	Me         tracker.User                `json:"me"`
	Items      []tracker.Item              `json:"items"`
	Details    map[string]tracker.Detail   `json:"details"`
	Changes    map[string][]tracker.Change `json:"changes"`
	Deployment tracker.Deployment          `json:"deployment"`
	// Error, when set, makes every network call fail with it (error-state demos).
	Error string `json:"error"`
}

// Fake implements tracker.Tracker.
type Fake struct {
	mu      sync.Mutex
	fx      Fixtures
	logPath string
	// Sent records every AddComment (tests read it).
	Sent []string
}

// New returns a tracker over fixtures held in memory.
func New(fx Fixtures) *Fake {
	if fx.Details == nil {
		fx.Details = map[string]tracker.Detail{}
	}
	return &Fake{fx: fx}
}

// Load reads fixtures from a file.
func Load(path string) (*Fake, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fx Fixtures
	if err := json.Unmarshal(raw, &fx); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f := New(fx)
	f.logPath = path + ".sent.log"
	return f, nil
}

func (f *Fake) fail() error {
	if f.fx.Error != "" {
		return fmt.Errorf("%s", f.fx.Error)
	}
	return nil
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Me(context.Context) (tracker.User, error) { return f.fx.Me, f.fail() }

func (f *Fake) MyItems(context.Context) ([]tracker.Item, error) {
	if err := f.fail(); err != nil {
		return nil, err
	}
	out := make([]tracker.Item, len(f.fx.Items))
	for i, it := range f.fx.Items {
		it.URL = f.URL(it.ID)
		out[i] = it
	}
	return out, nil
}

func (f *Fake) Item(_ context.Context, id string) (tracker.Detail, error) {
	if err := f.fail(); err != nil {
		return tracker.Detail{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.fx.Details[id]
	if !ok {
		for _, it := range f.fx.Items {
			if it.ID == id {
				d.Item = it
				ok = true
			}
		}
	}
	if !ok {
		return d, fmt.Errorf("item %s not found", id)
	}
	d.ID, d.URL = id, f.URL(id)
	return d, nil
}

func (f *Fake) Changes(_ context.Context, id string) ([]tracker.Change, error) {
	return f.fx.Changes[id], f.fail()
}

func (f *Fake) Deployment(context.Context) (tracker.Deployment, error) {
	return f.fx.Deployment, f.fail()
}

func (f *Fake) AddComment(_ context.Context, id, body string) error {
	if err := f.fail(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sent = append(f.Sent, id+": "+body)
	d := f.fx.Details[id]
	d.Comments = append(d.Comments, tracker.Comment{Author: f.fx.Me.Username, At: time.Now(), Body: body})
	f.fx.Details[id] = d
	if f.logPath != "" {
		if fh, err := os.OpenFile(f.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(fh, "%s #%s %q\n", time.Now().Format(time.RFC3339), id, body)
			fh.Close()
		}
	}
	return nil
}

func (f *Fake) URL(id string) string { return "https://tracker.invalid/items/" + id }

func (f *Fake) Ref(id string) string { return "#" + id }

var idPattern = regexp.MustCompile(`(?:^|[^\w/!\[&])#(\d{3,5})\b`)

func (f *Fake) IDPattern() *regexp.Regexp { return idPattern }
