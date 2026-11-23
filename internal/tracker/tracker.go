// Package tracker is the boundary between cairn and an issue tracker.
//
// The UI and the workflow logic only see the types in this file. Each tracker
// (GitLab today; GitHub, Jira, Notion later) lives in its own sub-package and
// implements Tracker. See README "Adapters" for how to add one.
package tracker

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// User is the person cairn runs for.
type User struct {
	Username string
	Name     string
}

// Item is one issue/ticket as shown in a list.
type Item struct {
	ID     string // tracker-native id: "681" (GitLab iid), "ABC-12" (Jira)…
	Title  string
	State  string // "opened", "closed"…
	Labels []string
	URL    string
}

// Image is a picture embedded in an item's description or comments. The
// adapter replaces it in the markdown with ImagePlaceholder(N, Alt).
type Image struct {
	N   int    // 1-based, unique across the whole Detail
	Alt string // alt text from the markdown
	Ref string // adapter-specific handle passed back to ImageFetcher
}

// Comment is one human comment on an item (system notes are dropped).
type Comment struct {
	Author string
	At     time.Time
	Body   string // markdown
}

// Detail is the full view of one item.
type Detail struct {
	Item
	Assignees   []string
	Description string // markdown, images replaced by placeholders
	Comments    []Comment
	Images      []Image
}

// Change is a merge request / pull request related to an item.
type Change struct {
	ID          string // "512" for !512
	Title       string
	State       string // "opened", "merged", "closed"
	MergeStatus string // tracker wording, e.g. "mergeable", "ci_must_pass"
	Pipeline    string // head pipeline status: "success", "failed", "running", "" = none
	Branch      string
	MergeCommit string // commit that landed on the target branch (merged only)
	MergedAt    time.Time
	URL         string
}

// Deployment says what the dev environment runs right now.
type Deployment struct {
	// Revision is the newest commit successfully deployed ("" = unknown).
	Revision string
	// InProgress is true while a newer deploy is running.
	InProgress bool
}

// Tracker is everything cairn needs from an issue tracker.
//
// All methods that talk to the network take a context and must honour its
// deadline; cairn calls them from background commands, never on the UI thread.
type Tracker interface {
	// Name is a short adapter name for the header ("gitlab").
	Name() string
	// Me is the authenticated user.
	Me(ctx context.Context) (User, error)
	// MyItems lists open items assigned to Me.
	MyItems(ctx context.Context) ([]Item, error)
	// Item loads one item with description, comments and images.
	Item(ctx context.Context, id string) (Detail, error)
	// Changes lists merge/pull requests related to an item.
	Changes(ctx context.Context, id string) ([]Change, error)
	// Deployment reports the dev environment (the "workflow mapping" side of
	// CI: what is deployed and whether a deploy runs).
	Deployment(ctx context.Context) (Deployment, error)
	// AddComment posts a comment as Me.
	AddComment(ctx context.Context, id, body string) error
	// URL is the web address of an item.
	URL(id string) string
	// Ref is how people write an item in prose: "#681", "ABC-12".
	Ref(id string) string
	// IDPattern finds item references in prose; submatch 1 is the id.
	IDPattern() *regexp.Regexp
}

// ImageFetcher is optionally implemented by adapters whose images need
// authentication to download. FetchImage returns a local file path.
type ImageFetcher interface {
	FetchImage(ctx context.Context, img Image) (string, error)
}

// ChangeFetcher is optionally implemented by adapters that can load one
// merge/pull request by id (the Inbox shows lone MRs from /actions).
type ChangeFetcher interface {
	Change(ctx context.Context, id string) (Change, error)
}

// ImagePlaceholder is the markdown an adapter puts where image N was.
func ImagePlaceholder(n int, alt string) string {
	if alt != "" {
		return fmt.Sprintf("**[obrázek %d]** _%s_ (o %d: otevřít)", n, alt, n)
	}
	return fmt.Sprintf("**[obrázek %d]** (o %d: otevřít)", n, n)
}
