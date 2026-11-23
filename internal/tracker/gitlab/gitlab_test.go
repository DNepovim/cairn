package gitlab

import (
	"context"
	"strings"
	"testing"

	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
)

func newTest(responses map[string]string) (*GitLab, *[]string) {
	cfg := config.Default()
	cfg.Tracker = config.Tracker{Type: "gitlab", Host: "git.example.com", Project: "grp/proj"}
	var calls []string
	g := New(cfg).WithRunner(func(_ context.Context, args ...string) ([]byte, error) {
		path := args[3]
		calls = append(calls, strings.Join(args, " "))
		for prefix, body := range responses {
			if strings.HasPrefix(path, prefix) {
				return []byte(body), nil
			}
		}
		return []byte(`[]`), nil
	})
	return g, &calls
}

func TestIDPattern_MatchesIssueRefsOnly(t *testing.T) {
	g, _ := newTest(nil)
	text := "Fix #681 and (#702), not !512, not a/b#999, not abc#123, `#555` in code, [#600](x)"
	got := tracker.Mentions(text, g.IDPattern())
	want := []string{"681", "702"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Mentions = %v, want %v", got, want)
	}
}

func TestLinkify_WrapsRefsOutsideCode(t *testing.T) {
	g, _ := newTest(nil)
	got := tracker.Linkify("See #681. `#682`", g.IDPattern(), g.URL)
	want := "See [#681](https://git.example.com/grp/proj/-/work_items/681). `#682`"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTidyMarkdown_NumbersImagesAcrossBlocks(t *testing.T) {
	n := 0
	desc, imgs := TidyMarkdown("A ![shot](/uploads/0123456789abcdef0123456789abcdef/a_b.png){width=50%}", &n, nil)
	note, imgs := TidyMarkdown("![](https://x/y.png)", &n, imgs)
	if !strings.Contains(desc, "[obrázek 1]") || strings.Contains(desc, "/uploads/") {
		t.Errorf("desc = %q", desc)
	}
	if !strings.Contains(note, "[obrázek 2]") {
		t.Errorf("note = %q", note)
	}
	if len(imgs) != 2 || imgs[0].Ref != "0123456789abcdef0123456789abcdef/a_b.png" || imgs[0].Alt != "shot" {
		t.Errorf("imgs = %+v", imgs)
	}
}

func TestMyItems_UsesAssigneeAndBuildsURLs(t *testing.T) {
	g, calls := newTest(map[string]string{
		"user":                  `{"username":"dominik","name":"D"}`,
		"projects/grp%2Fproj/i": `[{"iid":681,"title":"T","state":"opened","labels":["Bug"]}]`,
	})
	items, err := g.MyItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "681" || items[0].URL != "https://git.example.com/grp/proj/-/work_items/681" {
		t.Errorf("items = %+v", items)
	}
	if !strings.Contains(strings.Join(*calls, "\n"), "assignee_username=dominik") {
		t.Errorf("calls = %v", *calls)
	}
}

func TestDeployment_NewestSuccessAndRunning(t *testing.T) {
	g, _ := newTest(map[string]string{
		"projects/grp%2Fproj/pipelines?":       `[{"id":3,"sha":"c3","status":"running"},{"id":2,"sha":"c2","status":"failed"},{"id":1,"sha":"c1","status":"success"}]`,
		"projects/grp%2Fproj/pipelines/3/jobs": `[{"name":"build","status":"running"},{"name":"deploy-dev","status":"created"}]`,
	})
	d, err := g.Deployment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Revision != "c1" || !d.InProgress {
		t.Errorf("deployment = %+v", d)
	}
}

func TestAddComment_PostsBody(t *testing.T) {
	g, calls := newTest(nil)
	if err := g.AddComment(context.Background(), "681", "hello"); err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if !strings.Contains(got, "issues/681/notes -X POST -f body=hello") {
		t.Errorf("call = %q", got)
	}
}
