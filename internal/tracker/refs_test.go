package tracker

import (
	"regexp"
	"strings"
	"testing"
)

var hashID = regexp.MustCompile(`(?:^|[^\w/!\[&])#(\d{3,5})\b`)

func TestMentions_DistinctInOrder(t *testing.T) {
	got := Mentions("#702 then #681, again #702 and ```\n#999\n```", hashID)
	if strings.Join(got, ",") != "702,681" {
		t.Errorf("got %v", got)
	}
}

func TestTicketsInName_FirstWordOnly(t *testing.T) {
	cases := map[string]string{
		"#688 review the PR #700": "688",
		"review #700":             "",
		"ticket 612 spec":         "",
		"#612,#613":               "612,613",
		"":                        "",
	}
	for in, want := range cases {
		if got := strings.Join(TicketsInName(in, hashID), ","); got != want {
			t.Errorf("TicketsInName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripLeadingRef(t *testing.T) {
	if got := StripLeadingRef("#688 review", hashID); got != "review" {
		t.Errorf("got %q", got)
	}
	if got := StripLeadingRef("cleanup worktrees", hashID); got != "cleanup worktrees" {
		t.Errorf("got %q", got)
	}
}

func TestLinkify_KeepsLeadInAndExistingLinks(t *testing.T) {
	got := Linkify("(#681) [#682](u)", hashID, func(id string) string { return "U" + id })
	if got != "([#681](U681)) [#682](u)" {
		t.Errorf("got %q", got)
	}
}
