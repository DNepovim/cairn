package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/workflow"
)

// Update is the reducer: it applies one message and returns side effects.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.comment.SetWidth(max(10, m.width-6)) // inside a panel
		m.comment.SetHeight(max(3, min(12, m.height/3)))
		m.filter.Width = max(10, m.width-6)

	case tea.KeyMsg:
		cmd = m.handleKey(msg)

	case tea.MouseMsg:
		cmd = m.handleMouse(msg)

	case boardMsg:
		m.boardBusy = false
		m.boardAt = msg.at
		m.boardErr = msg.err
		if msg.err == nil {
			m.me = msg.me
			m.board = msg.data
			m.deployErr = msg.dErr
			m.recomputeRows()
			cmd = m.fetchExtras()
		}

	case extraMsg:
		delete(m.extraPending, msg.key)
		id := msg.key[2:]
		if msg.item != nil {
			m.extraItems[id] = *msg.item
		}
		if msg.change != nil {
			m.extraChanges[id] = *msg.change
		}
		m.version++

	case detailMsg:
		if msg.sel != m.sel {
			break // the user moved on while it loaded
		}
		m.detailBusy = false
		m.detailAt = msg.at
		m.detailErr = msg.err
		if msg.err == nil {
			d := msg.detail
			m.detail = &d
			m.detailChanges = msg.changes
			cmd = m.fetchImages(msg.sel, d.Images)
		}
		m.version++

	case imageMsg:
		if msg.sel == m.sel {
			m.images[msg.n] = msg.path
		}

	case bridgeReadyMsg:
		m.bridgeCh = msg.ch
		cmd = waitBridge(msg.ch, m.cfg.BridgeDir)

	case bridgeMsg:
		cmd = tea.Batch(m.applyBridge(msg.snap), m.fetchExtras())
		if m.bridgeCh != nil {
			cmd = tea.Batch(cmd, waitBridge(m.bridgeCh, m.cfg.BridgeDir))
		}

	case boardTickMsg:
		cmds := []tea.Cmd{tea.Tick(m.cfg.Refresh.Board(), func(time.Time) tea.Msg { return boardTickMsg{} })}
		if !m.boardBusy {
			m.boardBusy = true
			cmds = append(cmds, m.fetchBoard())
		}
		cmd = tea.Batch(cmds...)

	case detailTickMsg:
		cmds := []tea.Cmd{tea.Tick(m.cfg.Refresh.Detail(), func(time.Time) tea.Msg { return detailTickMsg{} })}
		if m.sel.kind == selTicket && !m.detailBusy {
			m.detailBusy = true
			cmds = append(cmds, m.fetchDetail(m.sel))
		}
		cmd = tea.Batch(cmds...)

	case commentSentMsg:
		if msg.err != nil {
			cmd = m.setStatus("komentář neodeslán: "+msg.err.Error(), true)
			m.mode = modeComment // keep the draft so nothing is lost
			cmd = tea.Batch(cmd, m.comment.Focus())
			break
		}
		m.comment.Reset()
		m.commentFor = ""
		cmd = tea.Batch(m.setStatus("komentář přidán k "+m.tr.Ref(msg.id), false), m.reloadDetail())

	case statusMsg:
		cmd = m.setStatus(msg.text, msg.err)

	case clearStatusMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
	}
	m.clampAll()
	return m, cmd
}

// recomputeRows derives "Moje tickety" from board data + bridge agents.
func (m *Model) recomputeRows() {
	byItem, unattached := workflow.AgentsByItem(m.snap.State.Agents, m.tr.IDPattern())
	m.agentsSide = unattached
	wf := m.cfg.Workflow
	rows := make([]workflow.Row, 0, len(m.board.items))
	for _, it := range m.board.items {
		in := workflow.Input{
			ID:                it.ID,
			Labels:            it.Labels,
			AgentRunning:      workflow.AnyRunning(byItem[it.ID]),
			Served:            m.board.served,
			Changes:           m.board.changes[it.ID],
			NewestMergedOnDev: m.board.onDev[it.ID],
			DeployInProgress:  m.board.deploy.InProgress,
		}
		f := workflow.Compute(in, wf)
		rows = append(rows, workflow.Row{Item: it, Flow: f, Rank: workflow.Rank(f, it.Labels, wf), Agents: byItem[it.ID]})
	}
	workflow.Sort(rows)
	// Keep the cursor on the same item across refreshes.
	var current string
	if es := m.entries(secTickets); m.cursor[secTickets] < len(es) {
		current = es[m.cursor[secTickets]].sel.id
	}
	// On the first load, start on the ticket already open in Detail (the
	// bridge often arrives before the board).
	if current == "" && m.sel.kind == selTicket {
		current = m.sel.id
	}
	m.rows = rows
	m.needs = workflow.NeedsMe(workflow.NeedsInput{
		Rows:       rows,
		Changes:    m.board.changes,
		Side:       m.snap.Side,
		Actions:    m.snap.Actions,
		UseActions: m.snap.ActionsFresh(m.opts.Now()),
		Ref:        m.tr.Ref,
	}, m.cfg.Workflow)
	m.inbox = workflow.Inbox(workflow.NeedsInput{
		Rows:       rows,
		Changes:    m.board.changes,
		Side:       m.snap.Side,
		Actions:    m.snap.Actions,
		UseActions: m.snap.ActionsFresh(m.opts.Now()),
		Ref:        m.tr.Ref,
	}, m.cfg.Workflow)
	m.version++ // Detail shows the actions at its top
	m.moveCursorTo(secTickets, selection{selTicket, current})
}

// applyBridge takes a new bridge snapshot; when the chat's active item
// changed, cairn follows it into Detail.
func (m *Model) applyBridge(s bridge.Snapshot) tea.Cmd {
	m.snap = s
	m.recomputeRows()
	m.version++ // agent lines in Detail may have changed
	a := s.State.Active
	if a == nil {
		return nil
	}
	key := a.Kind + ":" + a.ID
	if key == m.lastActive {
		return nil
	}
	m.lastActive = key
	// Follow the chat into Detail, but leave the list cursor alone while the
	// user is navigating.
	follow := m.opts.Now().Sub(m.navAt) > navGrace
	switch a.Kind {
	case "ticket":
		return m.open(selection{selTicket, strings.TrimPrefix(a.ID, "#")}, follow)
	case "side":
		m.setTab(secSide)
		return m.open(selection{selSide, a.ID}, follow)
	}
	return nil
}

// navGrace is how long after a manual cursor move the bridge may not move it.
const navGrace = 10 * time.Second

func (m *Model) touched() { m.navAt = m.opts.Now() }

// open shows sel in Detail; with moveCursor the matching list cursor follows.
func (m *Model) open(sel selection, moveCursor bool) tea.Cmd {
	if sel.kind == selNone {
		return nil
	}
	m.collapsed[secDetail] = false
	if moveCursor {
		m.moveCursorTo(secInbox, sel)
		switch sel.kind {
		case selTicket:
			m.moveCursorTo(secTickets, sel)
		case selSide, selAgent:
			m.moveCursorTo(secSide, sel)
		}
	}
	if sel == m.sel {
		return m.reloadDetail()
	}
	m.sel = sel
	m.detail, m.detailErr, m.detailChanges = nil, nil, nil
	m.images = map[int]string{}
	m.offset[secDetail] = 0
	m.version++
	return m.reloadDetail()
}

func (m *Model) reloadDetail() tea.Cmd {
	if m.sel.kind != selTicket {
		return nil
	}
	m.detailBusy = true
	return m.fetchDetail(m.sel)
}

func (m *Model) moveCursorTo(sec section, sel selection) {
	for i, e := range m.entries(sec) {
		if e.sel == sel {
			m.cursor[sec] = i
			m.ensureVisible(sec)
			return
		}
	}
}

// --- keys ---

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		return tea.Quit
	}
	// Fast typing (or tmux send-keys) can deliver several runes in one
	// message, e.g. "1z". Outside text inputs each rune is its own command.
	if k.Type == tea.KeyRunes && len(k.Runes) > 1 && !k.Paste && m.mode != modeFilter && m.mode != modeComment {
		var cmds []tea.Cmd
		for _, r := range k.Runes {
			cmds = append(cmds, m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
		}
		return tea.Batch(cmds...)
	}
	switch m.mode {
	case modeHelp:
		m.mode = modeNormal
		return nil
	case modeFilter:
		return m.keyFilter(k)
	case modeComment:
		return m.keyComment(k)
	case modeConfirm:
		return m.keyConfirm(k)
	case modeOpenPick:
		return m.keyOpenPick(k)
	}
	return m.keyNormal(k)
}

func (m *Model) keyNormal(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	switch key {
	case "q":
		return tea.Quit
	case "?":
		m.mode = modeHelp
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.moveTo(0)
	case "G", "end":
		m.moveTo(1 << 30)
	case "tab", "l", "right":
		m.cycleFocus(1)
	case "shift+tab", "h", "left":
		m.cycleFocus(-1)
	case "1":
		m.focus = secDetail
		m.setCollapsed(secDetail, false)
	case "2":
		// lands on the Inbox; pressing 2 again moves to the next tab
		if m.focus == secDetail {
			m.tab = secInbox
		} else {
			m.tab = nextTab(m.tab, 1)
		}
		m.focus = m.tab
		m.setCollapsed(m.tab, false)
	case "[", "]":
		d := 1
		if key == "[" {
			d = -1
		}
		m.setTab(nextTab(m.tab, d))
		m.focus = m.tab
		m.setCollapsed(m.tab, false)
	case "enter":
		if e, ok := m.current(); ok {
			return m.open(e.sel, true)
		}
	case "ctrl+d":
		m.scrollDetail(max(3, m.bodyHeight(secDetail)/2))
	case "ctrl+u":
		m.scrollDetail(-max(3, m.bodyHeight(secDetail)/2))
	case "J", "pgdown":
		m.scrollDetail(3)
	case "K", "pgup":
		m.scrollDetail(-3)
	case "z", " ":
		m.setCollapsed(m.focus, !m.collapsed[m.focus])
	case "o":
		if m.focus == secDetail && len(m.detailImages())+len(m.cache.links) > 0 {
			m.mode = modeOpenPick
			return nil
		}
		return m.openExternal(m.contextURL())
	case "y":
		if u := m.contextURL(); u != "" {
			return copyText(u)
		}
	case "Y":
		if id := m.contextRef(); id != "" {
			return copyText(id)
		}
	case "c":
		return m.startComment()
	case "r":
		m.boardBusy = true
		m.snap = bridge.Load(m.cfg.BridgeDir)
		return tea.Batch(m.fetchBoard(), m.reloadDetail(), m.setStatus("obnovuji…", false))
	case "/":
		m.mode = modeFilter
		m.filter.SetValue(m.filterText)
		m.filter.CursorEnd()
		return m.filter.Focus()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
		}
	}
	return nil
}

func (m *Model) keyFilter(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		m.filterText = ""
		m.filter.Blur()
		return nil
	case "enter":
		m.mode = modeNormal
		m.filter.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.filterText = m.filter.Value()
	for _, s := range tabs {
		m.cursor[s], m.offset[s] = 0, 0
	}
	return cmd
}

func (m *Model) startComment() tea.Cmd {
	// In "Moje tickety" the comment goes to the ticket under the cursor,
	// anywhere else to the ticket open in Detail.
	id := ""
	if e, ok := m.current(); ok && (m.focus == secTickets || m.focus == secInbox) && e.sel.kind == selTicket {
		id = e.sel.id
	} else if m.sel.kind == selTicket {
		id = m.sel.id
	}
	if id == "" {
		return m.setStatus("komentář jde jen k ticketu — vyber ticket", true)
	}
	if m.commentFor != id {
		m.comment.Reset()
	}
	m.commentFor = id
	m.mode = modeComment
	return m.comment.Focus()
}

func (m *Model) keyComment(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		m.comment.Blur()
		return m.setStatus("komentář zrušen (koncept zůstává)", false)
	case "ctrl+s":
		if strings.TrimSpace(m.comment.Value()) == "" {
			return m.setStatus("prázdný komentář", true)
		}
		m.mode = modeConfirm
		m.comment.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.comment, cmd = m.comment.Update(k)
	return cmd
}

func (m *Model) keyConfirm(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "y", "Y":
		m.mode = modeNormal
		return tea.Batch(m.sendComment(m.commentFor, strings.TrimSpace(m.comment.Value())), m.setStatus("odesílám…", false))
	case "n", "N", "esc":
		m.mode = modeComment
		return m.comment.Focus()
	}
	return nil
}

func (m *Model) keyOpenPick(k tea.KeyMsg) tea.Cmd {
	m.mode = modeNormal
	key := k.String()
	switch key {
	case "o", "enter":
		return m.openExternal(m.contextURL())
	case "esc":
		return nil
	}
	if n, err := strconv.Atoi(key); err == nil {
		if p, ok := m.images[n]; ok {
			return m.openExternal(p)
		}
		if u, ok := m.cache.links[n]; ok {
			return m.openExternal(u)
		}
		return m.setStatus(fmt.Sprintf("%d: obrázek ještě není stažený / není takový odkaz", n), true)
	}
	return nil
}

// --- movement ---

// cycleFocus walks Detail → Moje tickety → Bez ticketu (tabs count as stops).
func (m *Model) cycleFocus(d int) {
	m.focus = section((int(m.focus) + d + int(numSections)) % int(numSections))
	if m.focus != secDetail {
		m.tab = m.focus
	}
}

// setTab switches panel [2]'s tab; focus moves along if it was on the list.
func (m *Model) setTab(s section) {
	m.tab = s
	if m.focus != secDetail {
		m.focus = s
	}
}

// nextTab cycles through panel [2]'s tabs.
func nextTab(s section, d int) section {
	for i, t := range tabs {
		if t == s {
			return tabs[(i+d+len(tabs))%len(tabs)]
		}
	}
	return tabs[0]
}

// setCollapsed collapses a panel; both tabs share panel [2]'s state.
func (m *Model) setCollapsed(s section, v bool) {
	if s == secDetail {
		m.collapsed[secDetail] = v
		return
	}
	for _, t := range tabs {
		m.collapsed[t] = v
	}
}

func (m *Model) move(d int) {
	if m.focus == secDetail {
		m.scrollDetail(d)
		return
	}
	m.cursor[m.focus] += d
	m.touched()
	m.clamp(m.focus)
	m.ensureVisible(m.focus)
}

func (m *Model) moveTo(i int) {
	if m.focus == secDetail {
		m.offset[secDetail] = i
		m.clamp(secDetail)
		return
	}
	m.cursor[m.focus] = i
	m.touched()
	m.clamp(m.focus)
	m.ensureVisible(m.focus)
}

func (m *Model) scrollDetail(d int) {
	m.offset[secDetail] += d
	m.clamp(secDetail)
}

func (m *Model) clampAll() {
	for s := section(0); s < numSections; s++ {
		m.clamp(s)
		if s != secDetail {
			m.ensureVisible(s)
		}
	}
}

func (m *Model) clamp(s section) {
	if s == secDetail {
		maxOff := max(0, len(m.detailLines())-m.bodyHeight(secDetail))
		m.offset[s] = max(0, min(m.offset[s], maxOff))
		return
	}
	n := len(m.entries(s))
	m.cursor[s] = max(0, min(m.cursor[s], n-1))
}

// ensureVisible scrolls a list so the cursor's entry is fully in view.
func (m *Model) ensureVisible(s section) {
	h := m.bodyHeight(s)
	if h <= 0 {
		return
	}
	es := m.entries(s)
	start, height := 0, 1
	for i, e := range es {
		n := len(e.render(m.innerWidth(), i == m.cursor[s]))
		if i == m.cursor[s] {
			height = n
			break
		}
		start += n
	}
	if start < m.offset[s] {
		m.offset[s] = start
	}
	if start+height > m.offset[s]+h {
		m.offset[s] = start + height - h
	}
	total := 0
	for i, e := range es {
		total += len(e.render(m.innerWidth(), i == m.cursor[s]))
	}
	m.offset[s] = max(0, min(m.offset[s], total-h))
}

// --- mouse ---

func (m *Model) handleMouse(ev tea.MouseMsg) tea.Cmd {
	sec, line, ok := m.hit(ev.Y)
	if !ok {
		return nil
	}
	switch ev.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		d := 1
		if ev.Button == tea.MouseButtonWheelUp {
			d = -1
		}
		if sec == secDetail {
			m.scrollDetail(3 * d)
		} else {
			// lists scroll by moving their cursor, so the selection stays visible
			m.cursor[sec] += d
			m.touched()
			m.clamp(sec)
			m.ensureVisible(sec)
		}
	case tea.MouseButtonLeft:
		if ev.Action != tea.MouseActionPress {
			return nil
		}
		if line == -1 && sec != secDetail {
			// a click on a tab title selects that tab
			if t, ok := m.tabAt(ev.X); ok {
				m.tab, m.focus = t, t
				return nil
			}
		}
		m.focus = sec
		if line < 0 || sec == secDetail {
			if line == -1 {
				m.setCollapsed(sec, !m.collapsed[sec])
			}
			return nil
		}
		// find the entry under the click
		y := m.offset[sec] + line
		for i, e := range m.entries(sec) {
			n := len(e.render(m.innerWidth(), i == m.cursor[sec]))
			if y < n {
				m.cursor[sec] = i
				m.touched()
				return m.open(e.sel, true)
			}
			y -= n
		}
	}
	return nil
}
