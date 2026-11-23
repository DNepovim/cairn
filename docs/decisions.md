# Decisions

Why cairn looks and works the way it does. Newest first within each topic.

## Origin (2026-11-20 → 23)

- Started as a **Claude Code "ticket-pane" mod**: a side pane inside Claude Code
  showing the CorpLifting GitLab ticket Claude works on, Dominik's tickets with
  workflow states, subagents and their steps. Limits of the pane (one scroll
  for the whole tree, no independent sections, fixed placement, no images in
  tmux) led to a standalone TUI in a tmux pane Dominik can move and resize.
- **Bubble Tea** over gocui/lazygit's fork (2026-11-23): Dominik liked
  lazygit's look, but Bubble Tea is maintained, has ready components (lists,
  viewport, textarea) and its Elm architecture maps to React/Redux. "Doesn't
  have to look like lazygit" — but lazygit-style keys and `[n]` panel titles.
- Name **cairn** — a stack of stones marking the trail: where you are, what is
  done, the next step. Other projects share the name on GitHub (an agent
  system, a Claude skill); if it is ever published, use a distinct package name
  (e.g. `cairn-board`).
- **The Claude Code pane was removed** once cairn ran (2026-11-23). The mod
  stays only as the bridge (state.json), chat links/colours, `/actions` table
  rendering and `/cairn`.

## Layout

- Two panels: **[1] Detail** (own scroll) and **[2]** with tabs
  **Inbox — Moje tickety — Bez ticketu** (lazygit-style tabs in the title,
  `[`/`]`). Inbox is the default tab.
- **Inbox** = everything waiting on Dominik (tickets incl. unassigned ones, side
  items, lone MRs), in `actions.json` order. Tab titles show `needs-me/total`.
- Header is two borderless lines with a mini cairn (more rows for panels).
- Rows: title line tinted with the ticket's own colour, selected row stronger
  tint over all its lines; **no background on status lines** (Dominik asked to
  remove it). One colour per ticket everywhere: `palette[iid % 10]`, the same
  rule the chat uses.
- Notion notifications were tried and **removed** (2026-11-23): the Notion MCP
  has no inbox/unread API; watching Cloud Team requests was not what he wanted.

## Behaviour

- A bridge `active` change opens the item in Detail but does not move the list
  cursor if the user navigated in the last 10 s.
- Long URLs collapse to one line with `↗N`; `o N` opens.
- Fallback "needs me" when `actions.json` is missing or > 24 h old: 🧪, ✅,
  ✗ pipeline, `needsMeLabels`, side `needsYou`.
- Comments: `c`, `ctrl-s` asks y/n before posting.

## Multi-tracker

Planned from the start: Dominik wants cairn on other projects (GitHub, Jira,
Notion). Only GitLab is implemented; the interface and config are shaped so an
adapter is a new package + one case in `internal/adapters`.
