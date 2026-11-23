package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dominikblaha/cairn/internal/bridge"
	"github.com/dominikblaha/cairn/internal/tracker"
	"github.com/dominikblaha/cairn/internal/workflow"
)

// renderCache keeps the rendered Detail between frames. Markdown rendering is
// the expensive part of a frame, so it only reruns when the key changes.
type renderCache struct {
	key       string
	lines     []string
	renderers map[int]*glamour.TermRenderer // by wrap width
	// links are long URLs shortened in the rendered text, by their "o N"
	// number (numbered after the images).
	links map[int]string
}

func (c *renderCache) renderer(w int) *glamour.TermRenderer {
	if c.renderers == nil {
		c.renderers = map[int]*glamour.TermRenderer{}
	}
	if r, ok := c.renderers[w]; ok {
		return r
	}
	// The dark style without its 2-column document margin: in a 45-column
	// pane every column counts. A fixed style also avoids glamour querying
	// the terminal for its background, which can stall inside tmux.
	st := styles.DarkStyleConfig
	zero := uint(0)
	st.Document.Margin = &zero
	st.Document.BlockPrefix = ""
	st.Document.BlockSuffix = ""
	st.CodeBlock.Margin = &zero
	r, err := glamour.NewTermRenderer(glamour.WithStyles(st), glamour.WithWordWrap(w), glamour.WithEmoji())
	if err != nil {
		return nil
	}
	c.renderers[w] = r
	return r
}

// detailLines returns the rendered Detail content for the current width.
func (m *Model) detailLines() []string {
	w := m.innerWidth()
	key := fmt.Sprintf("%d|%v|%d", w, m.sel, m.version)
	if m.cache.key == key {
		return m.cache.lines
	}
	m.cache.links = map[int]string{}
	m.cache.lines = m.buildDetail(w)
	m.cache.key = key
	return m.cache.lines
}

func (m *Model) buildDetail(w int) []string {
	switch m.sel.kind {
	case selTicket:
		return m.buildTicket(w)
	case selSide:
		for _, it := range m.snap.Side {
			if it.ID == m.sel.id {
				return m.buildSide(it, w)
			}
		}
		return []string{sFaint.Render(m.sel.id + " už v side.json není")}
	case selChange:
		return m.buildChange(w)
	case selAgent:
		for _, a := range m.snap.State.Agents {
			if a.ID == m.sel.id {
				return m.buildAgent(a, w)
			}
		}
		return []string{sFaint.Render("subagent už skončil")}
	}
	return []string{sFaint.Render("Vyber položku a stiskni ⏎ — nebo jen piš v Claude Code o ticketu.")}
}

// wrap word-wraps styled text to w columns.
func wrap(s string, w int) []string {
	return strings.Split(ansi.Wrap(s, w, " -/"), "\n")
}

func (m *Model) buildTicket(w int) []string {
	id := m.sel.id
	ref := m.tr.Ref(id)
	d := m.detail
	if d == nil {
		if m.detailErr != nil {
			return wrap(sWarn.Render("⚠ "+ref+": "+m.detailErr.Error()), w)
		}
		return []string{sMuted.Render("Načítám " + ref + "…")}
	}
	var out []string
	add := func(lines ...string) { out = append(out, lines...) }
	add(m.needLines(m.sel, w)...)

	add(wrap(ticketStyle(id).Bold(true).Render(ref+" "+d.Title), w)...)

	// Board column as a chip, then the remaining labels as coloured tags.
	wf := m.cfg.Workflow
	var tagLine []string
	if stage := workflow.Stage(d.Labels, wf); stage != "" {
		tagLine = append(tagLine, chip(stage, lipgloss.Color("#5f5f87")))
	}
	if d.State != "" && d.State != "opened" {
		tagLine = append(tagLine, chip(d.State, lipgloss.Color("#875f5f")))
	}
	if t := renderTags(workflow.Tags(d.Labels, wf)); t != "" {
		tagLine = append(tagLine, t)
	}
	if len(tagLine) > 0 {
		add(wrap(strings.Join(tagLine, " "), w)...)
	}

	who := "—"
	if len(d.Assignees) > 0 {
		who = "@" + strings.Join(d.Assignees, ", @")
	}
	meta := sMuted.Render(who)
	if !m.detailAt.IsZero() {
		meta += sFaint.Render(" · ↻ " + m.detailAt.Format("15:04"))
	}
	add(wrap(meta, w)...)
	if m.detailErr != nil {
		add(wrap(sWarn.Render("⚠ "+m.detailErr.Error()), w)...)
	}

	// Subagents on this ticket (same lines as in the list).
	byItem, _ := workflow.AgentsByItem(m.snap.State.Agents, m.tr.IDPattern())
	for _, a := range byItem[id] {
		add(agentLines(a, id, m.tr.IDPattern(), "")...)
	}

	for _, c := range m.detailChanges {
		line := sBold.Render("!"+c.ID) + " " + mrStateStyle(c.State).Render(c.State)
		if c.MergeStatus != "" && c.State == "opened" {
			line += sFaint.Render(" · ") + sMuted.Render(c.MergeStatus)
		}
		if c.Pipeline != "" {
			line += sFaint.Render(" · ") + pipelineStyle(c.Pipeline).Render("● "+c.Pipeline)
		}
		add(fit(line+" "+sFaint.Render(c.Branch), w))
	}

	add("")
	desc := d.Description
	if strings.TrimSpace(desc) == "" {
		desc = "_(bez popisu)_"
	}
	add(m.markdown(m.shortenURLs(desc, w), w)...)

	if len(d.Comments) > 0 {
		add("", rule(fmt.Sprintf(" Komentáře (%d) ", len(d.Comments)), w))
	}
	for _, c := range d.Comments {
		at := ""
		if !c.At.IsZero() {
			at = c.At.Local().Format("2006-01-02 15:04")
		}
		add("", sAuthor.Render("@"+c.Author)+" "+sFaint.Render(at))
		add(m.markdown(m.shortenURLs(c.Body, w), w)...)
	}

	if len(d.Images) > 0 {
		add("", rule(" Obrázky ", w))
		for _, img := range d.Images {
			state := sFaint.Render("stahuji…")
			if _, ok := m.images[img.N]; ok {
				state = sOK.Render("o " + fmt.Sprint(img.N))
			}
			add(fit(fmt.Sprintf("[obrázek %d] %s ", img.N, img.Alt)+state, w))
		}
	}

	if refs := m.mentions(d); len(refs) > 0 {
		add("", sFaint.Render("↳ ")+strings.Join(refs, " "))
	}
	return out
}

// needLines head the Detail of an item that waits on me.
func (m *Model) needLines(sel selection, w int) []string {
	if !m.needsMe(sel) {
		return nil
	}
	acts := m.needs.Tickets[sel.id]
	if sel.kind == selSide {
		acts = m.needs.Side[sel.id]
	}
	if it, ok := m.inboxFor(sel); ok && len(it.Actions) > 0 {
		acts = it.Actions
	}
	if len(acts) == 0 {
		return []string{sNeed.Render("● čeká na tebe")}
	}
	var out []string
	for _, a := range acts {
		out = append(out, wrap(sNeed.Render("👉 "+strings.TrimSpace(a.Icon+" "+a.Action)), w)...)
	}
	return out
}

// mentions lists other tickets referenced in the description and comments,
// each in its own colour.
func (m *Model) mentions(d *tracker.Detail) []string {
	text := d.Description
	for _, c := range d.Comments {
		text += "\n" + c.Body
	}
	var out []string
	for _, id := range tracker.Mentions(text, m.tr.IDPattern()) {
		if id != d.ID {
			out = append(out, ticketStyle(id).Bold(true).Render(m.tr.Ref(id)))
		}
	}
	return out
}

func rule(label string, w int) string {
	return sFaint.Render("──") + sMuted.Render(label) + sFaint.Render(strings.Repeat("─", max(0, w-2-lipgloss.Width(label))))
}

// urlPattern finds bare URLs in markdown.
var urlPattern = regexp.MustCompile(`https?://[^\s<>()\[\]"'` + "`" + `]+`)

// shortenURLs replaces bare URLs too long for one line with a shortened,
// unbreakable form plus an "↗N" marker; "o N" opens the full URL. Without
// this, a long URL is hard-wrapped at arbitrary characters.
func (m *Model) shortenURLs(md string, w int) string {
	limit := w - 2
	locs := urlPattern.FindAllStringIndex(md, -1)
	if len(locs) == 0 {
		return md
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		u := md[loc[0]:loc[1]]
		// keep URLs that are a markdown link target "](…)" or "<…>", and short ones
		before := md[max(0, loc[0]-2):loc[0]]
		inFence := strings.Count(md[:loc[0]], "```")%2 == 1
		if len(u) <= limit || inFence || strings.HasSuffix(before, "](") || strings.HasSuffix(before, "<") {
			continue
		}
		n := m.nextLinkNumber()
		m.cache.links[n] = u
		marker := fmt.Sprintf(" ↗%d", n)
		short := ShortURL(u, limit-lipgloss.Width(marker))
		b.WriteString(md[last:loc[0]])
		b.WriteString("`" + short + "` **" + strings.TrimSpace(marker) + "**")
		last = loc[1]
	}
	b.WriteString(md[last:])
	return b.String()
}

func (m *Model) nextLinkNumber() int {
	n := len(m.cache.links) + 1
	if m.detail != nil && m.sel.kind == selTicket {
		n += len(m.detail.Images)
	}
	return n
}

// ShortURL drops the scheme and cuts the middle so the URL fits in w
// columns: "claude.ai/design/p/5672…share".
func ShortURL(u string, w int) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if len([]rune(s)) <= w || w < 8 {
		return s
	}
	r := []rune(s)
	tail := min(8, w/4)
	return string(r[:w-tail-1]) + "…" + string(r[len(r)-tail:])
}

// refPattern finds "#681"-style references in already rendered (ANSI) text,
// where the tracker's own pattern can't see word boundaries.
var refPattern = regexp.MustCompile(`#(\d{3,5})\b`)

// markdown renders md with glamour and colours ticket references.
func (m *Model) markdown(md string, w int) []string {
	r := m.cache.renderer(w)
	if r == nil {
		return wrap(md, w)
	}
	out, err := r.Render(md)
	if err != nil {
		return wrap(md, w)
	}
	out = strings.Trim(out, "\n")
	out = refPattern.ReplaceAllStringFunc(out, func(ref string) string {
		return ticketStyle(ref[1:]).Bold(true).Render(ref)
	})
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = fit(strings.TrimRight(l, " "), w)
		// glamour leaves double blank lines after code blocks; keep one
		if ansi.Strip(l) == "" && len(lines) > 0 && ansi.Strip(lines[len(lines)-1]) == "" {
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

func (m *Model) buildSide(it bridge.SideItem, w int) []string {
	icon := it.Icon
	if icon == "" {
		icon = "•"
	}
	out := append(m.needLines(m.sel, w), wrap(sSide.Render(it.ID+" "+it.Title), w)...)
	if it.Status != "" {
		out = append(out, wrap(icon+" "+sText.Render(it.Status), w)...)
	}
	if it.Note != "" {
		out = append(out, "")
		out = append(out, m.markdown(m.shortenURLs(it.Note, w), w)...)
	}
	if it.URL != "" {
		out = append(out, "", sFaint.Render("o otevřít · ")+sMuted.Render(it.URL))
	}
	return out
}

// buildChange shows a lone MR from the Inbox.
func (m *Model) buildChange(w int) []string {
	out := m.needLines(m.sel, w)
	c, ok := m.extraChanges[m.sel.id]
	if !ok {
		return append(out, sMuted.Render("Načítám !"+m.sel.id+"…"))
	}
	out = append(out, wrap(sBold.Render("!"+c.ID+" "+c.Title), w)...)
	line := mrStateStyle(c.State).Render(c.State)
	if c.MergeStatus != "" {
		line += sFaint.Render(" · ") + sMuted.Render(c.MergeStatus)
	}
	if c.Pipeline != "" {
		line += sFaint.Render(" · ") + pipelineStyle(c.Pipeline).Render("● "+c.Pipeline)
	}
	out = append(out, line, sFaint.Render(c.Branch))
	if c.URL != "" {
		out = append(out, "", sFaint.Render("o otevřít · ")+sMuted.Render(fit(c.URL, w-12)))
	}
	return out
}

func (m *Model) buildAgent(a bridge.Agent, w int) []string {
	out := wrap(sOK.Bold(true).Render("🤖 "+a.Description), w)
	out = append(out, sMuted.Render("stav: "+a.Status))
	if a.Step != "" {
		out = append(out, wrap(sMuted.Render("⎿ "+a.Step), w)...)
	}
	if len(a.Tickets) > 0 {
		out = append(out, sMuted.Render("tickety: "+strings.Join(a.Tickets, ", ")))
	}
	return out
}
