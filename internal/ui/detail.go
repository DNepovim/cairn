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
	add(m.markdown(desc, w)...)

	if len(d.Comments) > 0 {
		add("", rule(fmt.Sprintf(" Komentáře (%d) ", len(d.Comments)), w))
	}
	for _, c := range d.Comments {
		at := ""
		if !c.At.IsZero() {
			at = c.At.Local().Format("2006-01-02 15:04")
		}
		add("", sAuthor.Render("@"+c.Author)+" "+sFaint.Render(at))
		add(m.markdown(c.Body, w)...)
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
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = fit(strings.TrimRight(l, " "), w)
	}
	return lines
}

func (m *Model) buildSide(it bridge.SideItem, w int) []string {
	icon := it.Icon
	if icon == "" {
		icon = "•"
	}
	out := wrap(sSide.Render(it.ID+" "+it.Title), w)
	if it.Status != "" {
		out = append(out, wrap(icon+" "+sText.Render(it.Status), w)...)
	}
	if it.Note != "" {
		out = append(out, "")
		out = append(out, m.markdown(it.Note, w)...)
	}
	if it.URL != "" {
		out = append(out, "", sFaint.Render("o otevřít · ")+sMuted.Render(it.URL))
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
