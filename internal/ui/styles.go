package ui

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dominikblaha/cairn/internal/workflow"
)

// Colours. Ticket colours use the 16 ANSI slots so they match what the
// Claude Code mod draws in the chat with the same terminal theme.
var (
	accent   = lipgloss.Color("#d7875f") // terracotta: focus, keys
	muted    = lipgloss.Color("#8a8a8a")
	faint    = lipgloss.Color("#5f5f5f")
	text     = lipgloss.Color("#d0d0d0")
	warn     = lipgloss.Color("#d75f5f")
	ok       = lipgloss.Color("#87af5f")
	selectBg = lipgloss.Color("#3a3a3a")

	ticketPalette = []lipgloss.Color{"6", "5", "2", "4", "1", "3", "14", "13", "10", "12"}
)

// Shared styles.
var (
	sMuted   = lipgloss.NewStyle().Foreground(muted)
	sFaint   = lipgloss.NewStyle().Foreground(faint)
	sText    = lipgloss.NewStyle().Foreground(text)
	sBold    = lipgloss.NewStyle().Bold(true)
	sWarn    = lipgloss.NewStyle().Foreground(warn)
	sOK      = lipgloss.NewStyle().Foreground(ok)
	sKey     = lipgloss.NewStyle().Foreground(accent).Bold(true)
	sSide    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	sAuthor  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	sTitle   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	sSelLine = lipgloss.NewStyle().Background(selectBg)
	sNeed    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")).Bold(true)
)

// ticketColor gives every item one colour wherever it is drawn:
// palette[iid % len] for numeric ids (the mod's rule), a hash otherwise.
func ticketColor(id string) lipgloss.Color {
	if n, err := strconv.Atoi(id); err == nil && n >= 0 {
		return ticketPalette[n%len(ticketPalette)]
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	return ticketPalette[int(h.Sum32()%uint32(len(ticketPalette)))]
}

func ticketStyle(id string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ticketColor(id))
}

// chip renders a small coloured badge.
func chip(label string, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#1c1c1c")).Padding(0, 1).Render(label)
}

// flowChip renders a workflow state as icon + text in its colour (no
// background: the list stays calm, the colour carries the meaning).
func flowChip(f workflow.Flow) string {
	c := lipgloss.NewStyle()
	switch f.Kind {
	case workflow.Agent:
		c = c.Foreground(lipgloss.Color("#87afd7"))
	case workflow.LocalTest:
		c = c.Foreground(lipgloss.Color("#d7afff"))
	case workflow.ReviewMR:
		c = c.Foreground(lipgloss.Color("#ffd787"))
		if f.Failed {
			c = c.Foreground(lipgloss.Color("#ff8787"))
		}
	case workflow.Deploying:
		c = c.Foreground(lipgloss.Color("#87d7d7"))
	case workflow.DevTest:
		c = c.Foreground(lipgloss.Color("#afd787"))
	default:
		return sMuted.Render(f.Text)
	}
	return c.Render(f.Text)
}

// labelStyle colours a tag by what it means.
func labelStyle(tag string) lipgloss.Style {
	switch {
	case strings.HasPrefix(tag, "⚑ Highest"), strings.HasPrefix(tag, "⚑ High"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff875f")).Bold(true)
	case strings.HasPrefix(tag, "⚑ Average"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#d7d75f"))
	case strings.HasPrefix(tag, "⚑"):
		return sMuted
	case tag == "Bug":
		return lipgloss.NewStyle().Foreground(warn)
	case strings.HasPrefix(tag, "epic-"):
		return sFaint
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#87afaf"))
}

func pipelineStyle(status string) lipgloss.Style {
	switch status {
	case "success":
		return sOK
	case "failed":
		return sWarn
	case "running", "pending", "created":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#87d7d7"))
	}
	return sMuted
}

func mrStateStyle(state string) lipgloss.Style {
	switch state {
	case "opened":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd787")).Bold(true)
	case "merged":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#af87ff")).Bold(true)
	}
	return sMuted
}

// cairnArt is the logo: a two-line cairn of half-block stones, a pale
// pebble on a darker base.
var cairnArt = []struct {
	line  string
	color lipgloss.Color
}{
	{" ▗▆▖ ", "#d8d0c0"},
	{"▟███▙", "#8f8271"},
}

func renderArt() []string {
	out := make([]string, len(cairnArt))
	for i, a := range cairnArt {
		out[i] = lipgloss.NewStyle().Foreground(a.color).Render(a.line)
	}
	return out
}
