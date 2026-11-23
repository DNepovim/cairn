package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/tracker"
	"github.com/dominikblaha/cairn/internal/workflow"
)

const (
	headerHeight = 2 // borderless: mini cairn + two status lines
	panelGap     = 1 // blank lines before each section panel
)

// entry is one selectable item of a list section.
type entry struct {
	sel  selection
	url  string
	ref  string                            // what "Y" copies
	text string                            // what the filter matches
	draw func(w int, cursor bool) []string // rendered lines
}

func (e entry) render(w int, cursor bool) []string { return e.draw(w, cursor) }

// entries builds the visible (filtered) entries of a list section.
func (m *Model) entries(s section) []entry {
	var out []entry
	switch s {
	case secTickets:
		for _, r := range m.rows {
			out = append(out, m.ticketEntry(r))
		}
	case secSide:
		for _, it := range m.snap.Side {
			out = append(out, m.sideEntry(it))
		}
		for _, a := range m.agentsSide {
			out = append(out, m.agentEntry(a))
		}
	}
	if q := strings.ToLower(strings.TrimSpace(m.filterText)); q != "" {
		kept := out[:0]
		for _, e := range out {
			if strings.Contains(strings.ToLower(e.text), q) {
				kept = append(kept, e)
			}
		}
		out = kept
	}
	return out
}

// current returns the entry under the cursor of the focused list.
func (m *Model) current() (entry, bool) {
	if m.focus == secDetail {
		return entry{}, false
	}
	es := m.entries(m.focus)
	if c := m.cursor[m.focus]; c < len(es) {
		return es[c], true
	}
	return entry{}, false
}

// contextURL is what "o"/"y" act on: the focused list item, or the Detail item.
func (m *Model) contextURL() string {
	if e, ok := m.current(); ok {
		return e.url
	}
	switch m.sel.kind {
	case selTicket:
		return m.tr.URL(m.sel.id)
	case selSide:
		for _, it := range m.snap.Side {
			if it.ID == m.sel.id {
				return it.URL
			}
		}
	}
	return ""
}

func (m *Model) contextRef() string {
	if e, ok := m.current(); ok {
		return e.ref
	}
	switch m.sel.kind {
	case selTicket:
		return m.tr.Ref(m.sel.id)
	case selSide, selAgent:
		return m.sel.id
	}
	return ""
}

// prefix draws the two marker columns: cursor bar and "open in Detail".
func (m *Model) prefix(cursor bool, sel selection) string {
	c, o := " ", " "
	if cursor {
		c = sKey.Render("▌")
	}
	if m.sel == sel {
		o = sKey.Render("▶")
	}
	return c + o + " "
}

// needsMe reports whether an item waits on me.
func (m *Model) needsMe(sel selection) bool {
	switch sel.kind {
	case selTicket:
		_, ok := m.needs.Tickets[sel.id]
		return ok
	case selSide:
		_, ok := m.needs.Side[sel.id]
		return ok
	}
	return false
}

// needMark is the "●" column before an item that waits on me.
func (m *Model) needMark(sel selection) string {
	if m.needsMe(sel) {
		return sNeed.Render("●") + " "
	}
	return "  "
}

// actionLines are the /actions entries for an item: "👉 🧪 ① lokálně: …".
func actionLines(acts []bridge.Action, indent string) []string {
	var out []string
	for _, a := range acts {
		text := strings.TrimSpace(a.Icon + " " + a.Action)
		out = append(out, indent+sMuted.Render("👉 "+text))
	}
	return out
}

func (m *Model) ticketEntry(r workflow.Row) entry {
	sel := selection{selTicket, r.Item.ID}
	ref := m.tr.Ref(r.Item.ID)
	tags := workflow.Tags(r.Item.Labels, m.cfg.Workflow)
	pattern := m.tr.IDPattern()
	return entry{
		sel: sel, url: r.Item.URL, ref: ref,
		text: ref + " " + r.Item.Title + " " + strings.Join(tags, " "),
		draw: func(w int, cursor bool) []string {
			st := ticketStyle(r.Item.ID).Bold(cursor || m.sel == sel)
			lines := []string{m.prefix(cursor, sel) + m.needMark(sel) + st.Render(ref+" "+r.Item.Title)}
			lines = append(lines, actionLines(m.needs.Tickets[r.Item.ID], "     ")...)
			flow := "   " + flowChip(r.Flow)
			tagLine := renderTags(tags)
			if tagLine != "" && lipgloss.Width(flow)+2+lipgloss.Width(tagLine) <= w {
				lines = append(lines, flow+"  "+tagLine)
			} else {
				lines = append(lines, flow)
				if tagLine != "" {
					lines = append(lines, "   "+tagLine)
				}
			}
			for _, a := range r.Agents {
				lines = append(lines, agentLines(a, r.Item.ID, pattern, "     ")...)
			}
			return lines
		},
	}
}

func renderTags(tags []string) string {
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = labelStyle(t).Render(t)
	}
	return strings.Join(parts, sFaint.Render(" · "))
}

// agentLines draws a subagent under its ticket: icon + name, and its step
// while it runs.
func agentLines(a bridge.Agent, id string, pattern *regexp.Regexp, indent string) []string {
	name := tracker.StripLeadingRef(a.Description, pattern)
	icon := "🤖"
	st := ticketStyle(id)
	switch {
	case a.Status == "failed":
		icon, st = "✗", sWarn
	case a.Done():
		icon, st = "✓", sFaint
	}
	lines := []string{indent + st.Render(icon+" "+name)}
	if !a.Done() && a.Step != "" {
		lines = append(lines, indent+sMuted.Render("   ⎿ "+a.Step))
	}
	return lines
}

func (m *Model) sideEntry(it bridge.SideItem) entry {
	sel := selection{selSide, it.ID}
	icon := it.Icon
	if icon == "" {
		icon = "•"
	}
	return entry{
		sel: sel, url: it.URL, ref: it.ID, text: it.ID + " " + it.Title + " " + it.Status,
		draw: func(w int, cursor bool) []string {
			title := icon + " " + sSide.Render(it.ID) + " " + sText.Bold(m.sel == sel).Render(it.Title)
			lines := []string{m.prefix(cursor, sel) + m.needMark(sel) + title}
			lines = append(lines, actionLines(m.needs.Side[it.ID], "     ")...)
			if it.Status != "" {
				lines = append(lines, "     "+sMuted.Render(it.Status))
			}
			return lines
		},
	}
}

func (m *Model) agentEntry(a bridge.Agent) entry {
	sel := selection{selAgent, a.ID}
	return entry{
		sel: sel, ref: a.ID, text: a.Description + " " + a.Step,
		draw: func(w int, cursor bool) []string {
			lines := []string{m.prefix(cursor, sel) + "  " + sOK.Render("🤖 "+a.Description)}
			if a.Step != "" {
				lines = append(lines, "     "+sMuted.Render("⎿ "+a.Step))
			}
			return lines
		},
	}
}

// --- layout ---

// innerWidth is the text width inside a panel: "│ " … " │" costs 4 columns.
func (m *Model) innerWidth() int { return max(10, m.width-4) }

// frameHeight is the rows a section's panel uses besides its body.
func (m *Model) frameHeight(s section) int {
	if m.collapsed[s] {
		return 1 // a single title bar
	}
	return 2 // top + bottom border
}

// naturalHeight is how many body lines a section wants.
func (m *Model) naturalHeight(s section) int {
	if m.collapsed[s] {
		return 0
	}
	if s == secDetail {
		return max(1, len(m.detailLines()))
	}
	n := 0
	for i, e := range m.entries(s) {
		n += len(e.render(m.innerWidth(), i == m.cursor[s]))
	}
	return max(1, n)
}

// layout splits the rows between the section bodies: Detail gets up to ~55%,
// the lists share the rest by water-filling (small lists get all they need,
// the leftover goes to the big ones), and Detail takes whatever is left.
func (m *Model) layout() [numSections]int {
	var h [numSections]int
	avail := m.height - headerHeight - 1 // hint bar
	for _, s := range m.panels() {
		avail -= panelGap + m.frameHeight(s)
	}
	avail = max(0, avail)
	var need [numSections]int
	for _, s := range m.panels() {
		need[s] = m.naturalHeight(s)
	}
	lists := []section{m.tab}
	listNeeds := make([]int, len(lists))
	for i, s := range lists {
		listNeeds[i] = need[s]
	}
	target := 0
	if need[secDetail] > 0 {
		target = min(need[secDetail], avail*55/100)
	}
	got := waterfill(avail-target, listNeeds)
	used := 0
	for i, s := range lists {
		h[s] = got[i]
		used += got[i]
	}
	h[secDetail] = min(need[secDetail], avail-used)
	h[otherTab(m.tab)] = h[m.tab] // the hidden tab scrolls within the same panel
	return h
}

// panels are the visible panels top to bottom: [1] Detail, [2] the active tab.
func (m *Model) panels() []section { return []section{secDetail, m.tab} }

func (m *Model) bodyHeight(s section) int { return m.layout()[s] }

// waterfill gives each need at most a fair share of avail, redistributing
// what small needs leave unused.
func waterfill(avail int, needs []int) []int {
	out := make([]int, len(needs))
	left := avail
	remaining := 0
	for _, n := range needs {
		if n > 0 {
			remaining++
		}
	}
	for remaining > 0 && left > 0 {
		share := max(1, left/remaining)
		progressed := false
		for i, n := range needs {
			if out[i] >= n || left == 0 {
				continue
			}
			give := min(share, n-out[i], left)
			out[i] += give
			left -= give
			progressed = true
			if out[i] >= n {
				remaining--
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

// hit maps a screen row to (section, body line); line -1 is the title bar,
// -2 the bottom border.
func (m *Model) hit(y int) (section, int, bool) {
	if m.mode != modeNormal {
		return 0, 0, false
	}
	row := headerHeight
	h := m.layout()
	for _, s := range m.panels() {
		row += panelGap
		if y == row {
			return s, -1, true
		}
		row++
		if m.collapsed[s] {
			continue
		}
		if y < row+h[s] {
			return s, y - row, true
		}
		row += h[s]
		if y == row {
			return s, -2, true
		}
		row++
	}
	return 0, 0, false
}

// --- View ---

// View renders the whole screen, exactly m.height lines.
func (m Model) View() string {
	if m.width < 20 || m.height < 10 {
		return "cairn: okno je moc malé"
	}
	lines := m.viewHeader()
	switch m.mode {
	case modeHelp:
		lines = append(append(lines, ""), m.viewHelp()...)
	case modeComment, modeConfirm:
		lines = append(append(lines, ""), panel(sTitle.Render("✎ Komentář"), "", m.viewComment(), m.width, true)...)
	default:
		lines = append(lines, m.viewSections()...)
	}
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines[:m.height-1], m.viewHint())
	for i, l := range lines {
		lines[i] = fit(l, m.width)
	}
	return strings.Join(lines, "\n")
}

func fit(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func (m *Model) viewHeader() []string {
	project := m.cfg.Tracker.Project
	if i := strings.LastIndex(project, "/"); i >= 0 {
		project = project[i+1:]
	}
	user := "připojuji…"
	if m.me.Username != "" {
		user = "@" + m.me.Username
	}
	l1 := sTitle.Render("cairn") + sFaint.Render(" · "+m.tr.Name())
	if project != "" {
		l1 += " " + sText.Render(project)
	}
	l1 += sFaint.Render(" · ") + sMuted.Render(user)
	var l3 string
	switch {
	case m.boardErr != nil:
		l3 = sWarn.Render("⚠ " + m.boardErr.Error())
	case m.boardAt.IsZero():
		l3 = sMuted.Render("⟳ načítám board…")
	default:
		l3 = sMuted.Render("↻ " + m.boardAt.Format("15:04"))
		if m.boardBusy {
			l3 += sMuted.Render(" ⟳")
		}
		if m.deployErr != nil {
			l3 += sWarn.Render("  ⚠ deploy: " + m.deployErr.Error())
		}
	}
	var l4 string
	switch {
	case m.snap.ActionsErr != nil:
		l4 = sWarn.Render("⚠ actions.json: " + m.snap.ActionsErr.Error())
	case m.snap.StateErr != nil:
		l4 = sWarn.Render("⚠ state.json: " + m.snap.StateErr.Error())
	case m.snap.Missing:
		l4 = sFaint.Render("bridge: čekám na Claude Code")
	default:
		running := 0
		for _, a := range m.snap.State.Agents {
			if !a.Done() {
				running++
			}
		}
		l4 = sMuted.Render(fmt.Sprintf("🤖 %d · bridge %s", running, m.snap.State.UpdatedAt.Local().Format("15:04")))
	}
	art := renderArt()
	return []string{
		fit(art[0]+" "+l1, m.width),
		fit(art[1]+" "+l3+sFaint.Render(" · ")+l4, m.width),
	}
}

func (m *Model) viewSections() []string {
	h := m.layout()
	var out []string
	for _, s := range m.panels() {
		for i := 0; i < panelGap; i++ {
			out = append(out, "")
		}
		title, right := m.sectionTitle(s, h[s])
		focused := s == m.focus
		if m.collapsed[s] {
			out = append(out, titleBar(title, right, m.width, focused))
			continue
		}
		body := m.sectionBody(s)
		start := min(m.offset[s], max(0, len(body)-h[s]))
		visible := make([]string, h[s])
		for i := range visible {
			if start+i < len(body) {
				visible[i] = body[start+i]
			}
		}
		out = append(out, panel(title, right, visible, m.width, focused)...)
	}
	return out
}

// borderStyle is the frame colour: accent when focused, dim grey otherwise.
func borderStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().Foreground(accent)
	}
	return sFaint
}

// titleStyle styles a panel title according to focus.
func titleStyle(focused bool) lipgloss.Style {
	if focused {
		return sTitle
	}
	return sMuted.Bold(true)
}

// panel draws a rounded box w columns wide with the title embedded in the
// top border ("╭─ 2 Moje tickety ───── info ─╮") and body lines padded inside.
func panel(title, right string, body []string, w int, focused bool) []string {
	b := borderStyle(focused)
	inner := w - 4
	out := []string{topBorder(title, right, w, "╭", "╮", b)}
	for _, l := range body {
		l = fit(l, inner)
		out = append(out, b.Render("│")+" "+l+strings.Repeat(" ", max(0, inner-lipgloss.Width(l)))+" "+b.Render("│"))
	}
	return append(out, b.Render("╰"+strings.Repeat("─", max(0, w-2))+"╯"))
}

// titleBar is a collapsed section: just its title in a one-line frame.
func titleBar(title, right string, w int, focused bool) string {
	return topBorder(title, right, w, "╶", "╴", borderStyle(focused))
}

func topBorder(title, right string, w int, l, r string, b lipgloss.Style) string {
	head := b.Render(l+"─") + title + " "
	tail := ""
	if right != "" {
		tail = " " + right + " "
	}
	tail += b.Render("─" + r)
	fill := w - lipgloss.Width(head) - lipgloss.Width(tail)
	if fill < 1 {
		// too narrow for the info on the right: drop it
		tail = b.Render("─" + r)
		fill = max(0, w-lipgloss.Width(head)-lipgloss.Width(tail))
		head = fit(head, w-lipgloss.Width(tail)-fill)
	}
	return head + b.Render(strings.Repeat("─", fill)) + tail
}

// sectionBody returns all lines of a section (scrolling happens in viewSections).
func (m *Model) sectionBody(s section) []string {
	if s == secDetail {
		return m.detailLines()
	}
	es := m.entries(s)
	if len(es) == 0 {
		return []string{m.emptyText(s)}
	}
	var lines []string
	for i, e := range es {
		cursor := i == m.cursor[s]
		el := e.render(m.innerWidth(), cursor)
		if cursor && s == m.focus {
			for j := range el {
				el[j] = highlight(el[j], m.innerWidth())
			}
		}
		lines = append(lines, el...)
	}
	return lines
}

// highlight paints a selection background under an already-styled line by
// re-applying it after every reset.
func highlight(line string, w int) string {
	const bg = "\x1b[48;5;237m"
	line = fit(line, w)
	pad := strings.Repeat(" ", max(0, w-lipgloss.Width(line)))
	return bg + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bg) + pad + "\x1b[0m"
}

func (m *Model) emptyText(s section) string {
	filtered := m.filterText != ""
	switch {
	case filtered:
		return sFaint.Render("nic neodpovídá filtru")
	case s == secTickets && m.boardErr != nil:
		return sWarn.Render("⚠ board se nepodařilo načíst (r = znovu)")
	case s == secTickets && m.boardAt.IsZero():
		return sFaint.Render("Načítám…")
	}
	return sFaint.Render("Nic.")
}

// tabPrefix precedes the tab names in panel [2]'s border: "╭─[2] Moje…".
const (
	tabPrefix = "[2] "
	tabSep    = " — "
)

// tabLabels are the two tab names with their counts.
func (m *Model) tabLabels() (string, string) {
	l1 := fmt.Sprintf("%s (%d/%d)", sectionNames[secTickets], len(m.needs.Tickets), len(m.rows))
	l2 := fmt.Sprintf("%s (%d/%d)", sectionNames[secSide], len(m.needs.Side), len(m.snap.Side)+len(m.agentsSide))
	// "╭─" + "[2] " + tabs + " ─╮" plus room for a filter indicator
	if 2+len(tabPrefix)+lipgloss.Width(l1+tabSep+l2)+4 > m.width {
		l1 = fmt.Sprintf("Moje (%d/%d)", len(m.needs.Tickets), len(m.rows))
	}
	return l1, l2
}

// tabAt maps a click column on panel [2]'s border to a tab.
func (m *Model) tabAt(x int) (section, bool) {
	l1, l2 := m.tabLabels()
	start := 2 + lipgloss.Width(tabPrefix) // after "╭─"
	if x >= start && x < start+lipgloss.Width(l1) {
		return secTickets, true
	}
	start += lipgloss.Width(l1) + lipgloss.Width(tabSep)
	if x >= start && x < start+lipgloss.Width(l2) {
		return secSide, true
	}
	return 0, false
}

// sectionTitle returns the styled title and the right-hand info of a panel:
// "[1] Detail", or "[2] Moje tickety (9) — Bez ticketu (7)" with the active
// tab highlighted.
func (m *Model) sectionTitle(s section, bodyH int) (string, string) {
	focused := s == m.focus
	collapsed := ""
	if m.collapsed[s] {
		collapsed = " ▸"
	}
	var title, right string
	if s == secDetail {
		title = titleStyle(focused).Render("[1] "+sectionNames[secDetail]) + sMuted.Render(collapsed)
		right = m.detailHeaderInfo(bodyH)
	} else {
		l1, l2 := m.tabLabels()
		active := titleStyle(true) // the active tab is always accent + bold
		if !focused {
			active = sText.Bold(true)
		}
		inactive := sFaint
		t1, t2 := inactive.Render(l1), inactive.Render(l2)
		if m.tab == secTickets {
			t1 = active.Render(l1)
		} else {
			t2 = active.Render(l2)
		}
		title = titleStyle(focused).Render(tabPrefix) + t1 + sFaint.Render(tabSep) + t2 + sMuted.Render(collapsed)
		if m.filterText != "" {
			right = "/" + m.filterText
		}
	}
	if right != "" {
		right = titleStyle(focused).UnsetBold().Render(right)
	}
	return title, right
}

func (m *Model) detailHeaderInfo(bodyH int) string {
	var label string
	switch m.sel.kind {
	case selTicket:
		label = m.tr.Ref(m.sel.id)
	case selSide, selAgent:
		label = m.sel.id
	}
	total := len(m.detailLines())
	if bodyH > 0 && total > bodyH {
		pct := 100 * (m.offset[secDetail] + bodyH) / total
		label += fmt.Sprintf(" %d%%", min(100, pct))
	}
	if m.detailBusy {
		label += " ⟳"
	}
	return label
}

// --- hint bar & overlays ---

type hint struct{ key, label string }

func renderHints(hs []hint) string {
	parts := make([]string, len(hs))
	for i, h := range hs {
		parts[i] = sKey.Render(h.key) + " " + sMuted.Render(h.label)
	}
	return strings.Join(parts, sFaint.Render(" · "))
}

func (m *Model) viewHint() string {
	if m.status != "" {
		if m.statusErr {
			return sWarn.Render("⚠ " + m.status)
		}
		return sOK.Render("✓ " + m.status)
	}
	switch m.mode {
	case modeFilter:
		return m.filter.View()
	case modeOpenPick:
		return renderHints([]hint{{"o/⏎", "prohlížeč"}, {"1-9", "obrázek/odkaz ↗"}, {"esc", "zpět"}})
	case modeComment:
		return renderHints([]hint{{"ctrl+s", "odeslat"}, {"esc", "zrušit"}})
	case modeConfirm:
		return renderHints([]hint{{"y", "odeslat"}, {"n", "upravit"}})
	case modeHelp:
		return sMuted.Render("libovolná klávesa zavře nápovědu")
	}
	if m.focus == secDetail {
		return renderHints([]hint{{"j/k", "scroll"}, {"^d/^u", "půl str."}, {"o", "otevřít"}, {"c", "koment"}, {"2", "seznam"}, {"?", "help"}})
	}
	hs := []hint{{"j/k", "pohyb"}, {"⏎", "detail"}, {"[ ]", "tab"}, {"o", "otevřít"}, {"c", "koment"}}
	return renderHints(append(hs, hint{"/", "filtr"}, hint{"?", "help"}))
}

var helpKeys = []hint{
	{"j k ↓ ↑", "pohyb v seznamu / scroll detailu"},
	{"g G", "začátek / konec"},
	{"1 2", "panel Detail / seznam (2 znovu: přepne tab)"},
	{"[ ]", "tab Moje tickety ↔ Bez ticketu"},
	{"tab ⇧tab h l", "Detail → Moje → Bez ticketu → …"},
	{"⏎ / klik", "otevřít položku v Detailu"},
	{"^d ^u J K", "scroll Detailu (odkudkoli)"},
	{"z mezera", "sbalit / rozbalit sekci"},
	{"o", "otevřít v prohlížeči"},
	{"o 1…9", "v Detailu: obrázek N / odkaz ↗N"},
	{"y Y", "kopírovat URL / ID"},
	{"c", "napsat komentář (ctrl+s, pak y)"},
	{"r", "obnovit vše"},
	{"/ esc", "filtr seznamů / zrušit filtr"},
	{"kolečko", "scroll pod myší"},
	{"q", "konec"},
}

var helpLegend = []string{
	"🤖 pracuje subagent",
	"🧪 připraveno k lokálnímu testu",
	"📝 MR otevřený (✗ pipeline spadla)",
	"🚀 nasazuje se na dev",
	"✅ připraveno k testu na devu",
	"⏸ nic se neděje, sloupec z boardu",
	"⚑ priorita · ▶ otevřené v Detailu",
	"● čeká na tebe (actions.json z /actions,",
	"   jinak 🧪 ✅ ✗ pipeline / needsMeLabels)",
}

func (m *Model) viewHelp() []string {
	w := m.width
	var b strings.Builder
	b.WriteString(sTitle.Render("Klávesy") + "\n")
	keyW := 13
	for _, h := range helpKeys {
		b.WriteString(sKey.Render(fmt.Sprintf("%-*s", keyW, h.key)) + sText.Render(h.label) + "\n")
	}
	b.WriteString("\n" + sTitle.Render("Legenda") + "\n")
	for _, l := range helpLegend {
		b.WriteString(sMuted.Render(l) + "\n")
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1).Width(w - 2)
	return strings.Split(box.Render(strings.TrimRight(b.String(), "\n")), "\n")
}

func (m *Model) viewComment() []string {
	ref := m.tr.Ref(m.commentFor)
	title := ""
	for _, r := range m.rows {
		if r.Item.ID == m.commentFor {
			title = r.Item.Title
		}
	}
	if title == "" && m.detail != nil && m.detail.ID == m.commentFor {
		title = m.detail.Title
	}
	lines := []string{
		ticketStyle(m.commentFor).Bold(true).Render("✎ Komentář k " + ref + " " + title),
		sMuted.Render("odejde jako @" + m.me.Username + " přes " + m.tr.Name()),
		"",
	}
	lines = append(lines, strings.Split(m.comment.View(), "\n")...)
	if m.mode == modeConfirm {
		q := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(warn).Padding(0, 1).
			Render(sBold.Render("Odeslat komentář k "+ref+"?") + "  " + sKey.Render("y") + sMuted.Render(" ano  ") + sKey.Render("n") + sMuted.Render(" ne"))
		lines = append(append(lines, ""), strings.Split(q, "\n")...)
	}
	return lines
}

// detailImages lists the images of the current ticket detail.
func (m *Model) detailImages() []tracker.Image {
	if m.sel.kind != selTicket || m.detail == nil {
		return nil
	}
	return m.detail.Images
}
