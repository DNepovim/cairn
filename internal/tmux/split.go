// Package tmux opens cairn in a side pane.
package tmux

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNotInTmux is returned by Split outside a tmux session.
var ErrNotInTmux = errors.New("cairn --split needs tmux: start tmux first (or run plain `cairn` in a terminal split)")

// SplitArgs builds the tmux command that opens self in a right-hand pane
// taking widthPct% of the window, in dir, with extra args passed through.
func SplitArgs(self, dir string, widthPct int, extra []string) []string {
	cmd := []string{shellQuote(self)}
	for _, a := range extra {
		cmd = append(cmd, shellQuote(a))
	}
	return []string{"split-window", "-h", "-l", strconv.Itoa(widthPct) + "%", "-c", dir, strings.Join(cmd, " ")}
}

// Split runs SplitArgs when inside tmux.
func Split(extra []string) error {
	if os.Getenv("TMUX") == "" {
		return ErrNotInTmux
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir, _ := os.Getwd()
	out, err := exec.Command("tmux", SplitArgs(self, dir, 33, extra)...).CombinedOutput()
	if err != nil {
		return errors.New("tmux split-window: " + strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " '\"\\$`!*?&;|<>(){}[]#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
