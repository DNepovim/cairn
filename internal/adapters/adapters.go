// Package adapters picks the tracker implementation named in the config.
//
// Adding a tracker: implement tracker.Tracker in internal/tracker/<name>
// and add a case below. See README "Adapters".
package adapters

import (
	"fmt"

	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
	"github.com/dominikblaha/cairn/internal/tracker/fake"
	"github.com/dominikblaha/cairn/internal/tracker/gitlab"
)

// New returns the tracker for cfg.Tracker.Type.
func New(cfg config.Config) (tracker.Tracker, error) {
	switch cfg.Tracker.Type {
	case "gitlab":
		return gitlab.New(cfg), nil
	case "fake":
		if cfg.Tracker.Fixtures == "" {
			return fake.New(fake.Fixtures{}), nil
		}
		return fake.Load(cfg.Tracker.Fixtures)
	case "github", "jira", "notion":
		return nil, fmt.Errorf("tracker %q is planned but not implemented yet (see README: Adapters)", cfg.Tracker.Type)
	}
	return nil, fmt.Errorf("unknown tracker type %q", cfg.Tracker.Type)
}
