// Package gitlab implements tracker.Tracker on top of the `glab` CLI.
//
// Shelling out to `glab api` reuses the user's existing glab login (token,
// host config, VPN quirks) instead of handling credentials in cairn.
package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
)

// Runner executes `glab` with args and returns stdout. Tests swap it out.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// GitLab is the adapter. Create it with New.
type GitLab struct {
	host     string
	project  string // "group/project"
	wf       config.Workflow
	run      Runner
	cacheDir string

	mu sync.Mutex
	me *tracker.User
}

// New builds an adapter for cfg.Tracker.
func New(cfg config.Config) *GitLab {
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return &GitLab{
		host:     cfg.Tracker.Host,
		project:  cfg.Tracker.Project,
		wf:       cfg.Workflow,
		run:      execGlab,
		cacheDir: filepath.Join(cache, "cairn", "images"),
	}
}

// WithRunner replaces the glab runner (tests).
func (g *GitLab) WithRunner(r Runner) *GitLab { g.run = r; return g }

func execGlab(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "glab", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = "timeout: " + msg
		}
		return nil, errors.New(firstLine(msg))
	}
	return out, nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// api calls `glab api --hostname HOST path [extra…]` and decodes JSON into v.
func (g *GitLab) api(ctx context.Context, v any, path string, extra ...string) error {
	args := append([]string{"api", "--hostname", g.host, path}, extra...)
	out, err := g.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("glab api %s: %w", shortPath(path), err)
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("glab api %s: bad JSON: %w", shortPath(path), err)
	}
	return nil
}

// shortPath drops the "projects/<encoded>/" prefix from error messages.
func shortPath(p string) string {
	if i := strings.Index(p, "/"); strings.HasPrefix(p, "projects/") && i > 0 {
		if j := strings.Index(p[i+1:], "/"); j > 0 {
			return p[i+1+j+1:]
		}
	}
	return p
}

func (g *GitLab) proj() string { return "projects/" + url.PathEscape(g.project) }

// Name implements tracker.Tracker.
func (g *GitLab) Name() string { return "gitlab" }

// --- JSON shapes (only the fields cairn reads) ---

type apiUser struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

type apiIssue struct {
	IID         int       `json:"iid"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	Labels      []string  `json:"labels"`
	Description string    `json:"description"`
	Assignees   []apiUser `json:"assignees"`
	WebURL      string    `json:"web_url"`
}

type apiNote struct {
	Body      string    `json:"body"`
	System    bool      `json:"system"`
	CreatedAt time.Time `json:"created_at"`
	Author    apiUser   `json:"author"`
}

type apiMR struct {
	IID                 int        `json:"iid"`
	Title               string     `json:"title"`
	State               string     `json:"state"`
	DetailedMergeStatus string     `json:"detailed_merge_status"`
	SourceBranch        string     `json:"source_branch"`
	MergeCommitSHA      string     `json:"merge_commit_sha"`
	SquashCommitSHA     string     `json:"squash_commit_sha"`
	MergedAt            *time.Time `json:"merged_at"`
	WebURL              string     `json:"web_url"`
	HeadPipeline        *struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
}

type apiPipeline struct {
	ID     int    `json:"id"`
	SHA    string `json:"sha"`
	Status string `json:"status"`
}

type apiJob struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Me implements tracker.Tracker (cached after the first call).
func (g *GitLab) Me(ctx context.Context) (tracker.User, error) {
	g.mu.Lock()
	cached := g.me
	g.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	var u apiUser
	if err := g.api(ctx, &u, "user"); err != nil {
		return tracker.User{}, err
	}
	me := tracker.User{Username: u.Username, Name: u.Name}
	g.mu.Lock()
	g.me = &me
	g.mu.Unlock()
	return me, nil
}

// MyItems implements tracker.Tracker.
func (g *GitLab) MyItems(ctx context.Context) ([]tracker.Item, error) {
	me, err := g.Me(ctx)
	if err != nil {
		return nil, err
	}
	var issues []apiIssue
	path := fmt.Sprintf("%s/issues?assignee_username=%s&state=opened&per_page=100", g.proj(), url.QueryEscape(me.Username))
	if err := g.api(ctx, &issues, path); err != nil {
		return nil, err
	}
	items := make([]tracker.Item, 0, len(issues))
	for _, i := range issues {
		items = append(items, g.item(i))
	}
	return items, nil
}

func (g *GitLab) item(i apiIssue) tracker.Item {
	id := fmt.Sprint(i.IID)
	return tracker.Item{ID: id, Title: i.Title, State: i.State, Labels: i.Labels, URL: g.URL(id)}
}

// Item implements tracker.Tracker.
func (g *GitLab) Item(ctx context.Context, id string) (tracker.Detail, error) {
	var (
		issue    apiIssue
		notes    []apiNote
		errIssue error
		errNotes error
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); errIssue = g.api(ctx, &issue, g.proj()+"/issues/"+id) }()
	go func() {
		defer wg.Done()
		errNotes = g.api(ctx, &notes, g.proj()+"/issues/"+id+"/notes?sort=asc&per_page=100")
	}()
	wg.Wait()
	if errIssue != nil {
		return tracker.Detail{}, errIssue
	}
	d := tracker.Detail{Item: g.item(issue)}
	for _, a := range issue.Assignees {
		d.Assignees = append(d.Assignees, a.Username)
	}
	// Images are numbered across description and comments so "o 3" is unambiguous.
	n := 0
	d.Description, d.Images = TidyMarkdown(issue.Description, &n, d.Images)
	if errNotes != nil {
		// The description is still useful; surface the failure as a comment.
		d.Comments = append(d.Comments, tracker.Comment{Author: "cairn", Body: "⚠ komentáře se nepodařilo načíst: " + errNotes.Error()})
		return d, nil
	}
	for _, note := range notes {
		if note.System {
			continue
		}
		var body string
		body, d.Images = TidyMarkdown(note.Body, &n, d.Images)
		d.Comments = append(d.Comments, tracker.Comment{Author: note.Author.Username, At: note.CreatedAt, Body: body})
	}
	return d, nil
}

// upload matches GitLab image uploads: ![alt](/uploads/<secret>/<name>){width=…}
var upload = regexp.MustCompile(`!\[([^\]]*)\]\(/uploads/([0-9a-f]{16,64})/([^)\s]+)\)(\{[^}]*\})?`)

// otherImage matches any other inline image (external URLs).
var otherImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]*)\)(\{[^}]*\})?`)

// TidyMarkdown replaces GitLab uploads with numbered placeholders (the
// terminal can't follow their relative links) and records them as images.
// n is the running image counter shared across one Detail.
func TidyMarkdown(md string, n *int, images []tracker.Image) (string, []tracker.Image) {
	md = upload.ReplaceAllStringFunc(md, func(m string) string {
		sub := upload.FindStringSubmatch(m)
		*n++
		images = append(images, tracker.Image{N: *n, Alt: sub[1], Ref: sub[2] + "/" + sub[3]})
		return tracker.ImagePlaceholder(*n, sub[1])
	})
	md = otherImage.ReplaceAllStringFunc(md, func(m string) string {
		sub := otherImage.FindStringSubmatch(m)
		*n++
		images = append(images, tracker.Image{N: *n, Alt: sub[1], Ref: sub[2]})
		return tracker.ImagePlaceholder(*n, sub[1])
	})
	return strings.TrimSpace(md), images
}

// Changes implements tracker.Tracker.
func (g *GitLab) Changes(ctx context.Context, id string) ([]tracker.Change, error) {
	var mrs []apiMR
	if err := g.api(ctx, &mrs, g.proj()+"/issues/"+id+"/related_merge_requests"); err != nil {
		return nil, err
	}
	out := make([]tracker.Change, 0, len(mrs))
	for _, m := range mrs {
		out = append(out, change(m))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Change implements tracker.ChangeFetcher.
func (g *GitLab) Change(ctx context.Context, id string) (tracker.Change, error) {
	var m apiMR
	if err := g.api(ctx, &m, g.proj()+"/merge_requests/"+id); err != nil {
		return tracker.Change{}, err
	}
	return change(m), nil
}

func change(m apiMR) tracker.Change {
	c := tracker.Change{
		ID:          fmt.Sprint(m.IID),
		Title:       m.Title,
		State:       m.State,
		MergeStatus: m.DetailedMergeStatus,
		Branch:      m.SourceBranch,
		URL:         m.WebURL,
		MergeCommit: m.MergeCommitSHA,
	}
	if c.MergeCommit == "" {
		c.MergeCommit = m.SquashCommitSHA
	}
	if m.HeadPipeline != nil {
		c.Pipeline = m.HeadPipeline.Status
	}
	if m.MergedAt != nil {
		c.MergedAt = *m.MergedAt
	}
	return c
}

// Deployment implements tracker.Tracker: the newest successful pipeline on
// the deploy ref is what runs on dev; the newest pipeline still running
// (and, with a deploy job configured, whose deploy job hasn't finished) means
// a deploy is in progress.
func (g *GitLab) Deployment(ctx context.Context) (tracker.Deployment, error) {
	var pipes []apiPipeline
	path := fmt.Sprintf("%s/pipelines?ref=%s&per_page=5", g.proj(), url.QueryEscape(g.wf.DeployRef))
	if err := g.api(ctx, &pipes, path); err != nil {
		return tracker.Deployment{}, err
	}
	var d tracker.Deployment
	for _, p := range pipes {
		if p.Status == "success" {
			d.Revision = p.SHA
			break
		}
	}
	if len(pipes) > 0 && isRunning(pipes[0].Status) {
		d.InProgress = true
		if g.wf.DeployJob != "" {
			var jobs []apiJob
			if err := g.api(ctx, &jobs, fmt.Sprintf("%s/pipelines/%d/jobs?per_page=100", g.proj(), pipes[0].ID)); err == nil {
				for _, j := range jobs {
					if j.Name == g.wf.DeployJob && !isRunning(j.Status) && j.Status != "manual" {
						d.InProgress = false // deploy job already finished (or was skipped)
					}
				}
			}
		}
	}
	return d, nil
}

func isRunning(status string) bool {
	switch status {
	case "running", "pending", "created", "waiting_for_resource", "preparing":
		return true
	}
	return false
}

// AddComment implements tracker.Tracker.
func (g *GitLab) AddComment(ctx context.Context, id, body string) error {
	return g.api(ctx, nil, g.proj()+"/issues/"+id+"/notes", "-X", "POST", "-f", "body="+body)
}

// URL implements tracker.Tracker.
func (g *GitLab) URL(id string) string {
	return fmt.Sprintf("https://%s/%s/-/work_items/%s", g.host, g.project, id)
}

// Ref implements tracker.Tracker.
func (g *GitLab) Ref(id string) string { return "#" + id }

// idPattern: "#681" not preceded by a word char, "/" or "!" (that would be a
// path or an MR). Go has no look-behind, so the boundary char is matched.
var idPattern = regexp.MustCompile(`(?:^|[^\w/!\[&])#(\d{3,5})\b`)

// IDPattern implements tracker.Tracker.
func (g *GitLab) IDPattern() *regexp.Regexp { return idPattern }

// FetchImage implements tracker.ImageFetcher: uploads are downloaded once
// through glab (they need the user's token) into the cache directory.
func (g *GitLab) FetchImage(ctx context.Context, img tracker.Image) (string, error) {
	if strings.HasPrefix(img.Ref, "http://") || strings.HasPrefix(img.Ref, "https://") {
		return img.Ref, nil // external picture: `open` handles URLs directly
	}
	secret, name, ok := strings.Cut(img.Ref, "/")
	if !ok {
		return "", fmt.Errorf("bad image ref %q", img.Ref)
	}
	safe := regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(name, "_")
	file := filepath.Join(g.cacheDir, secret+"-"+safe)
	if st, err := os.Stat(file); err == nil && st.Size() > 0 {
		return file, nil
	}
	out, err := g.run(ctx, "api", "--hostname", g.host, fmt.Sprintf("%s/uploads/%s/%s", g.proj(), secret, name))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(g.cacheDir, 0o755); err != nil {
		return "", err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return "", err
	}
	return file, os.Rename(tmp, file)
}
