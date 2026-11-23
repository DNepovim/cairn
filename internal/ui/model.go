// Package ui is cairn's Bubble Tea program: one Model, messages for every
// background result, and pure rendering in View.
//
// For a React developer: Model is the state, Update is the reducer (it
// returns the next state plus side effects as tea.Cmd), View renders the
// state to a string. Network calls never run in Update; they run inside
// tea.Cmd functions and come back as messages.
package ui

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/config"
	"github.com/dominikblaha/cairn/internal/tracker"
	"github.com/dominikblaha/cairn/internal/workflow"
)

// section is one of the stacked panels.
type section int

const (
	secDetail section = iota
	secTickets
	secSide
	numSections
)

var sectionNames = [numSections]string{"Detail", "Moje tickety", "Bez ticketu"}

// selKind says what the Detail panel shows.
type selKind int

const (
	selNone selKind = iota
	selTicket
	selSide
	selAgent
)

type selection struct {
	kind selKind
	id   string // ticket id, side id or agent id
}

// mode is the input mode; each mode owns the keyboard.
type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeComment
	modeConfirm
	modeHelp
	modeOpenPick // after "o" in Detail: pick browser or image N
)

// boardData is the raw result of a board refresh (flows are derived from it
// together with the bridge's agents in recomputeRows).
type boardData struct {
	items   []tracker.Item
	changes map[string][]tracker.Change
	onDev   map[string]bool // newest merged change is deployed
	deploy  tracker.Deployment
	served  string
}

// Model is the whole application state.
type Model struct {
	cfg  config.Config
	tr   tracker.Tracker
	opts Options

	width, height int
	focus         section
	collapsed     [numSections]bool
	cursor        [numSections]int
	offset        [numSections]int // first visible line per section
	mode          mode

	// tracker data
	me         tracker.User
	board      boardData
	rows       []workflow.Row
	boardAt    time.Time
	boardErr   error
	deployErr  error
	boardBusy  bool
	agentsSide []bridge.Agent // running agents without a ticket

	// bridge data
	snap       bridge.Snapshot
	lastActive string // "ticket:681" — the active item we last followed
	bridgeCh   <-chan struct{}

	// detail
	sel           selection
	detail        *tracker.Detail
	detailChanges []tracker.Change
	detailAt      time.Time
	detailErr     error
	detailBusy    bool
	images        map[int]string // image N -> local file
	version       int            // bumps whenever detail content changes
	cache         *renderCache   // pointer: View may fill it

	// input widgets
	filter     textinput.Model
	filterText string
	comment    textarea.Model
	commentFor string // ticket id the draft belongs to

	status    string
	statusErr bool
	statusID  int
}

// Options tweak behaviour for tests and demos.
type Options struct {
	// Exec runs external commands (open, pbcopy). Tests record instead.
	Exec func(name string, args ...string) error
	// NoWatch skips the fsnotify watcher (tests).
	NoWatch bool
}

// New builds the initial model.
func New(cfg config.Config, tr tracker.Tracker, opts Options) Model {
	if opts.Exec == nil {
		opts.Exec = func(name string, args ...string) error { return exec.Command(name, args...).Start() }
	}
	fi := textinput.New()
	fi.Prompt = "/ "
	fi.Placeholder = "filtr…"
	ta := textarea.New()
	ta.Placeholder = "Napiš komentář (markdown)…"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	m := Model{
		cfg:     cfg,
		tr:      tr,
		opts:    opts,
		focus:   secTickets,
		filter:  fi,
		comment: ta,
		images:  map[int]string{},
		cache:   &renderCache{},
		width:   60,
		height:  40,
	}
	m.snap = bridge.Load(cfg.BridgeDir)
	return m
}

// --- messages ---

type boardMsg struct {
	me   tracker.User
	data boardData
	err  error
	dErr error
	at   time.Time
}

type detailMsg struct {
	sel     selection
	detail  tracker.Detail
	changes []tracker.Change
	err     error
	at      time.Time
}

type imageMsg struct {
	sel  selection
	n    int
	path string
}

type bridgeMsg struct{ snap bridge.Snapshot }
type bridgeReadyMsg struct{ ch <-chan struct{} }
type boardTickMsg struct{}
type detailTickMsg struct{}
type commentSentMsg struct {
	id  string
	err error
}
type statusMsg struct {
	text string
	err  bool
}
type clearStatusMsg struct{ id int }

// Init starts the first refreshes, the bridge watcher and the polling ticks.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.fetchBoard(),
		tea.Tick(m.cfg.Refresh.Board(), func(time.Time) tea.Msg { return boardTickMsg{} }),
		tea.Tick(m.cfg.Refresh.Detail(), func(time.Time) tea.Msg { return detailTickMsg{} }),
		func() tea.Msg { return bridgeMsg{snap: bridge.Load(m.cfg.BridgeDir)} },
	}
	if !m.opts.NoWatch {
		cmds = append(cmds, startWatch(m.cfg.BridgeDir))
	}
	return tea.Batch(cmds...)
}

// --- commands (side effects) ---

func startWatch(dir string) tea.Cmd {
	return func() tea.Msg {
		ch, _, err := bridge.Watch(dir)
		if err != nil {
			return statusMsg{text: "bridge: " + err.Error(), err: true}
		}
		return bridgeReadyMsg{ch: ch}
	}
}

// waitBridge blocks (in its own goroutine) until the watcher signals.
func waitBridge(ch <-chan struct{}, dir string) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return bridgeMsg{snap: bridge.Load(dir)}
	}
}

// fetchBoard loads my items, their changes, the deployment and the local
// server, all off the UI thread.
func (m Model) fetchBoard() tea.Cmd {
	tr, wf := m.tr, m.cfg.Workflow
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		msg := boardMsg{at: time.Now()}
		me, err := tr.Me(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.me = me
		items, err := tr.MyItems(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		d := boardData{items: items, changes: map[string][]tracker.Change{}, onDev: map[string]bool{}}
		var wg sync.WaitGroup
		var mu sync.Mutex
		wg.Add(2)
		go func() { defer wg.Done(); d.deploy, msg.dErr = tr.Deployment(ctx) }()
		go func() { defer wg.Done(); d.served = workflow.LocalServer(ctx, wf.LocalTestPort) }()
		sem := make(chan struct{}, 6) // at most 6 glab processes at once
		for _, it := range items {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ch, err := tr.Changes(ctx, id)
				if err != nil {
					return // the item still shows, just without MR info
				}
				mu.Lock()
				d.changes[id] = ch
				mu.Unlock()
			}(it.ID)
		}
		wg.Wait()
		for id, ch := range d.changes {
			if nm := workflow.NewestMerged(ch); nm != nil {
				d.onDev[id] = workflow.IsAncestor(ctx, wf.RepoPath, nm.MergeCommit, d.deploy.Revision)
			}
		}
		msg.data = d
		return msg
	}
}

func (m Model) fetchDetail(sel selection) tea.Cmd {
	if sel.kind != selTicket {
		return nil
	}
	tr := m.tr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		msg := detailMsg{sel: sel, at: time.Now()}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); msg.detail, msg.err = tr.Item(ctx, sel.id) }()
		go func() { defer wg.Done(); msg.changes, _ = tr.Changes(ctx, sel.id) }()
		wg.Wait()
		return msg
	}
}

// fetchImages downloads every image of the detail, one message per image.
func (m Model) fetchImages(sel selection, imgs []tracker.Image) tea.Cmd {
	f, ok := m.tr.(tracker.ImageFetcher)
	if !ok || len(imgs) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(imgs))
	for _, img := range imgs {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			p, err := f.FetchImage(ctx, img)
			if err != nil {
				return nil
			}
			return imageMsg{sel: sel, n: img.N, path: p}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) sendComment(id, body string) tea.Cmd {
	tr := m.tr
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return commentSentMsg{id: id, err: tr.AddComment(ctx, id, body)}
	}
}

// openExternal opens a URL or file with macOS `open`.
func (m Model) openExternal(target string) tea.Cmd {
	run := m.opts.Exec
	return func() tea.Msg {
		if target == "" {
			return statusMsg{text: "není co otevřít", err: true}
		}
		if err := run("open", target); err != nil {
			return statusMsg{text: "open: " + err.Error(), err: true}
		}
		return statusMsg{text: "otevřeno: " + target}
	}
}

// copyText puts text on the clipboard with pbcopy.
func copyText(text string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return statusMsg{text: "pbcopy: " + err.Error(), err: true}
		}
		return statusMsg{text: "zkopírováno: " + text}
	}
}

func (m *Model) setStatus(text string, isErr bool) tea.Cmd {
	m.status, m.statusErr = text, isErr
	m.statusID++
	id := m.statusID
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{id: id} })
}
