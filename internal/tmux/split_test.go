package tmux

import (
	"strings"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	got := strings.Join(SplitArgs("/usr/local/bin/cairn", "/work dir", 33, []string{"--config", "/a b.json"}), "|")
	want := "split-window|-h|-l|33%|-c|/work dir|/usr/local/bin/cairn --config '/a b.json'"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestSplit_OutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if err := Split(nil); err != ErrNotInTmux {
		t.Errorf("err = %v", err)
	}
}
