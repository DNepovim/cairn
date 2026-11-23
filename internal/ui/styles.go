package ui

import (
	"fmt"
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

// paletteIndex is the mod's colour rule: iid % len for numeric ids, a hash
// otherwise.
func paletteIndex(id string) int {
	if n, err := strconv.Atoi(id); err == nil && n >= 0 {
		return n % len(ticketPalette)
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	return int(h.Sum32() % uint32(len(ticketPalette)))
}

// ticketColor gives every item one colour wherever it is drawn.
func ticketColor(id string) lipgloss.Color { return ticketPalette[paletteIndex(id)] }

// paletteHex approximates the ANSI palette slots in RGB, as the base for
// row tints (ANSI slots themselves can't be mixed).
var paletteHex = []string{"#2aa1b3", "#c061cb", "#33b864", "#4a78f0", "#d0443e", "#c9a227", "#33c7de", "#e07ae8", "#57d98a", "#6f96ff"}

// sideTintKey selects the side items' yellow instead of a ticket colour.
const sideTintKey = "side"

// rowTint is the background behind a list row in its item's colour: a faint
// wash for title lines, a stronger one for the selected row. Adaptive, so it
// stays readable on dark and light terminals.
func rowTint(key string, strong bool) lipgloss.AdaptiveColor {
	base := "#c9a227"
	if key != sideTintKey {
		base = paletteHex[paletteIndex(key)]
	}
	dark, light := 0.20, 0.14
	if strong {
		dark, light = 0.40, 0.32
	}
	return lipgloss.AdaptiveColor{Dark: mix(base, "#121212", dark), Light: mix(base, "#ffffff", light)}
}

// mix blends hex colour a into b by t (0 = b, 1 = a).
func mix(a, b string, t float64) string {
	pa, pb := rgb(a), rgb(b)
	var out [3]int
	for i := range out {
		out[i] = int(float64(pa[i])*t + float64(pb[i])*(1-t) + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

func rgb(h string) [3]int {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b)
	return [3]int{r, g, b}
}

// paint puts a background under an already-styled line, full width, by
// re-applying it after every reset inside the line.
func paint(line string, w int, bg lipgloss.TerminalColor) string {
	probe := lipgloss.NewStyle().Background(bg).Render("x")
	i := strings.Index(probe, "x")
	if i <= 0 {
		return line // no colour support (e.g. tests): leave the line alone
	}
	seq := probe[:i]
	line = fit(line, w)
	pad := strings.Repeat(" ", max(0, w-lipgloss.Width(line)))
	return seq + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+seq) + pad + "\x1b[0m"
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
