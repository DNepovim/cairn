# Backlog

Ordered. Ask Dominik before starting an item that changes behaviour he uses.

1. **Package as a Claude Code plugin** (`claude-plugin/`): the mod from
   `claude-plugin/ticket-pane` (rename to `cairn`), plus the generic skills
   `standup`, `checkout`, `actions`, a generic `cairn-flow` skill (order of
   work, context-first ticket pickup, approvals, lessons, knowledge base upkeep)
   and the agents `builder`, `code-reviewer`, `browser-tester`,
   `product-reviewer` made project-agnostic (project specifics from
   `.claude/cairn.json` / the project's CLAUDE.md). Today these skills live in
   the CorpLifting repo (`.claude/skills/`, `.claude/agents/`); they move only
   after the plugin works there, then get deleted from that repo in a reviewed
   MR. Distribution: the organisation's plugin library on claude.ai (publish
   only on his OK) or a GitHub marketplace repo.
2. **`cairn status --json` / `cairn actions --json`**: let the skills read board
   data through cairn's adapters instead of calling `glab` themselves.
3. **Binary distribution**: goreleaser + Homebrew tap; the plugin checks the
   binary exists and prints the install command (never installs by itself).
4. **Unverified by hand**: `o` (browser) and `y` (copy), images on a real
   ticket (`o N`), mouse click on tab titles, light-terminal tints.
5. Rare glitch: once the ticket list scrolled past its cursor (not reproduced).
6. Mouse capture disables terminal text selection (`--no-mouse` works around).
7. Adapters: github (`gh`), jira (REST v3, non-numeric ids → hash colours),
   notion (database as tracker).
8. Ancestry checks only see commits already fetched into `repoPath`.
