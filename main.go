// Command cairn is a terminal sidebar for ticket work: my tickets and where
// they stand, the ticket Claude Code is talking about, and
// work without a ticket.
//
//	cairn                 run the TUI here
//	cairn --split         open cairn in a new tmux pane on the right (33%)
//	cairn --config FILE   use a specific config file
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dominikblaha/cairn/internal/adapters"
	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tmux"
	"github.com/dominikblaha/cairn/internal/ui"
)

var version = "dev" // set by -ldflags at release time

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cairn:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("cairn", flag.ContinueOnError)
	split := fs.Bool("split", false, "open cairn in a new tmux pane on the right")
	cfgPath := fs.String("config", "", "config file (default: .claude/cairn.json upward, then ~/.config/cairn/config.json)")
	showVersion := fs.Bool("version", false, "print version")
	noMouse := fs.Bool("no-mouse", false, "disable mouse support (keeps terminal text selection)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: cairn [split] [--split] [--config FILE] [--no-mouse]")
		fs.PrintDefaults()
	}
	// "cairn split" is an alias of "cairn --split".
	if len(args) > 0 && args[0] == "split" {
		args = append([]string{"--split"}, args[1:]...)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println("cairn", version)
		return nil
	}
	if *split {
		var pass []string
		if *cfgPath != "" {
			pass = append(pass, "--config", *cfgPath)
		}
		if *noMouse {
			pass = append(pass, "--no-mouse")
		}
		return tmux.Split(pass)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("%w\nsee examples/corplifting.cairn.json for a starting point", err)
	}
	tr, err := adapters.New(cfg)
	if err != nil {
		return err
	}
	// Ask the terminal for its background now, before Bubble Tea owns stdin
	// (the answer is cached; row tints adapt to dark/light from it).
	lipgloss.HasDarkBackground()
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if !*noMouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err = tea.NewProgram(ui.New(cfg, tr, ui.Options{}), opts...).Run()
	return err
}
