package tracker

import (
	"regexp"
	"strings"
)

// codeOrLink matches spans in which references are not linkified: fenced code,
// inline code and existing markdown links.
var codeOrLink = regexp.MustCompile("(?s)```.*?```|`[^`\\n]*`|\\[[^\\]\\n]*\\]\\([^)\\n]*\\)")

// Mentions returns the distinct ids that pattern finds in text, in order of
// first appearance, ignoring code spans and existing links.
func Mentions(text string, pattern *regexp.Regexp) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range splitOutsideCode(text) {
		if part.code {
			continue
		}
		for _, m := range pattern.FindAllStringSubmatch(part.text, -1) {
			if id := m[1]; !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// Linkify rewrites every reference outside code into a markdown link built by
// urlFor, e.g. "#681" → "[#681](https://…/681)". Text before the reference
// that the pattern consumed (a leading space) is kept.
func Linkify(text string, pattern *regexp.Regexp, urlFor func(id string) string) string {
	var b strings.Builder
	for _, part := range splitOutsideCode(text) {
		if part.code {
			b.WriteString(part.text)
			continue
		}
		last := 0
		for _, loc := range pattern.FindAllStringSubmatchIndex(part.text, -1) {
			// loc[0..1] whole match, loc[2..3] the id. The visible reference
			// starts after any lead-in the pattern matched (see refStart).
			start := refStart(part.text, loc)
			id := part.text[loc[2]:loc[3]]
			b.WriteString(part.text[last:start])
			b.WriteString("[" + part.text[start:loc[1]] + "](" + urlFor(id) + ")")
			last = loc[1]
		}
		b.WriteString(part.text[last:])
	}
	return b.String()
}

// refStart skips the non-reference lead-in of a match (patterns cannot use
// look-behind in Go, so they match one boundary character instead).
func refStart(s string, loc []int) int {
	start := loc[0]
	for start < loc[2] && !isRefChar(s[start]) {
		start++
	}
	return start
}

func isRefChar(c byte) bool {
	return c == '#' || c == '!' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

type textPart struct {
	text string
	code bool
}

func splitOutsideCode(text string) []textPart {
	var parts []textPart
	last := 0
	for _, loc := range codeOrLink.FindAllStringIndex(text, -1) {
		parts = append(parts, textPart{text[last:loc[0]], false}, textPart{text[loc[0]:loc[1]], true})
		last = loc[1]
	}
	return append(parts, textPart{text[last:], false})
}

// briefPattern finds the tickets a subagent brief is about: "#629", "issue 629",
// "ticket #629", "tiket 629" (Czech).
var briefPattern = regexp.MustCompile(`(?i)(?:#|\bissue\s+#?|\bticket\s+#?|\btiket\s+#?)(\d{3,4})\b`)

// TicketsInName returns ticket ids an agent's name starts with ("#688 review"
// → ["688"]). Only the first word counts, like the mod, so a brief that merely
// mentions another ticket does not attach the agent to it.
func TicketsInName(description string, pattern *regexp.Regexp) []string {
	first, _, _ := strings.Cut(strings.TrimSpace(description), " ")
	if first == "" {
		return nil
	}
	if ids := Mentions(first, pattern); len(ids) > 0 {
		return ids
	}
	return Mentions(first, briefPattern)
}

// StripLeadingRef removes a leading "#688 " from an agent name for display.
func StripLeadingRef(description string, pattern *regexp.Regexp) string {
	first, rest, ok := strings.Cut(strings.TrimSpace(description), " ")
	if ok && len(Mentions(first, pattern)) > 0 {
		return strings.TrimSpace(rest)
	}
	return description
}
