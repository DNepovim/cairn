# Backlog

Ordered. Ask Dominik before starting an item that changes behaviour he uses.

1. **Package as a Claude Code plugin — monorepo** (decided 2026-11-23). Target layout:
   ```
   cairn/
     main.go, internal/…              Go app (unchanged)
     plugin/                          Claude Code plugin (from claude-plugin/ticket-pane, renamed cairn)
       .claude-plugin/plugin.json
       hooks/hooks.json, register.tsx bridge writer, chat links/colours, /actions table, /cairn
       types/index.d.ts
       skills/standup, checkout, actions, cairn-flow
       agents/builder, code-reviewer, browser-tester, product-reviewer
     .claude-plugin/marketplace.json  the repo is its own marketplace
     docs/
   ```
   Install: `/plugin marketplace add ~/projects/cairn` → `/plugin install cairn@cairn`
   (replaces `claude --plugin-dir …`). Why one repo: the bridge contract
   (`state.json`, `side.json`, `actions.json`) changes on both sides in one
   commit/version.
   **Verify first (risks):**
   - Security policy for hook modules of an *installed* plugin: during mod
     development a `session.append` hook was skipped by `cc-plugin-sec-default`.
     Check every hook still runs installed: bridge write (`state.json`), chat
     link/colour rewrite, `/actions` table render, `/cairn` command, `tool.call`
     step capture, `agent.spawn` mapping.
   - Skill namespace: plugin skills are namespaced (`/cairn:standup`). Check
     whether plain `/standup` resolves, and what wins when a project has a
     skill of the same name (CorpLifting still has `standup`/`actions` until
     step 2 below).
   **Skills/agents must be generalised before they move:** today they hardcode
   `glab`, `yetanothercorplifting/corplifting`, labels, `deploy-dev`, :5013,
   Adéla/Dan. They should read data via `cairn status --json` / `cairn actions
   --json` (item 2) and project specifics from `.claude/cairn.json` + the
   project's CLAUDE.md. Then delete them from the CorpLifting repo in a reviewed
   MR (never merge without his explicit "merge"). The generic `cairn-flow`
   skill takes from CorpLifting's `corplifting-sdlc`: Order of work, Kontext at
   ticket pickup, approvals (GitLab writes, ①/②, never merge without OK),
   learning from returned tickets (lessons), knowledge base upkeep.
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
