# cairn

A terminal sidebar for ticket work, built with Go and Bubble Tea. It runs in a
narrow tmux pane (45–80 columns, about a third of the window) next to Claude
Code. Like lazygit, it is made of numbered, bordered panels: **[1] Detail**,
and **[2]**, which has three tabs: *Inbox*, *Moje tickety* and *Bez ticketu*
(`╭─[2] Inbox (10) — Moje tickety (5/9) — Bez ticketu (3/7) ─╮`). When the
pane is narrow, the titles shorten to `Inbox 10 · Moje 5/9 · Bez 3/7`.

0. **Inbox** (the default tab, and where `2` lands) is everything waiting on
   you, in the order of `actions.json`. That includes tickets not assigned to
   you and lone MRs, which cairn loads through the tracker. Each row shows the
   icon, the coloured ID and title, and a `👉 action` line. With no fresh
   `actions.json`, it lists your tickets that need you (in board order), then
   side items. When it is empty: *Na tebe teď nic nečeká ✓*.

1. **Detail**: the selected item. For a ticket that is the title in its own
   colour, labels, assignees, the subagents working on it, related MRs with
   their state and pipeline, then the description and every comment rendered
   as markdown. Images show up as `[obrázek N] (o N: otevřít)`. For a side
   item it shows the title, status, note and URL. Detail scrolls on its own.
2. **Moje tickety** (tab of [2]): open issues assigned to me, each with its workflow state:

   | | meaning |
   |---|---|
   | 🤖 pracuje subagent | a running Claude Code subagent is attached |
   | 🧪 připraveno k lokálnímu testu | the local test port (5013) is served from this ticket's worktree |
   | 📝 MR !N otevřený (✗ pipeline) | an MR is open (its pipeline failed) |
   | 🚀 nasazuje se na dev | merged, but not on dev yet, and a deploy is running |
   | ✅ připraveno k testu na devu | merged and deployed, still in a developer column |
   | ⏸ *column* | nothing is happening; the board column is shown |

   Active items come first, then by priority, then bugs, then newest. Tags,
   `⚑` priority and subagents (`⎿ current step`) appear under each ticket.
3. **Bez ticketu** (tab of [2]): side items (B1, B2…) and running subagents that have no
   ticket.

**Needs me.** The tab titles count what waits on you out of the total, e.g.
`Moje tickety (3/9) — Bez ticketu (2/8)`. Those rows get a bright `●`, plus a
dim `👉 …` line with the action, which is also shown at the top of Detail.
cairn takes this from `actions.json` (see the bridge section below). If that
file is missing or older than 24 h, it falls back to: 🧪, ✅, an MR with a
✗ pipeline, a label from `needsMeLabels`, or a side item with `"needsYou": true`.

The header is two lines: a small cairn, then user, last refresh, running
agents and the bridge time.

Rows are tinted: each ticket's title line gets a faint background in its own
colour (side items use yellow). The selected row gets a stronger tint across
all its lines. Tints adapt to dark and light terminals.

Each ticket keeps one colour everywhere (`palette[iid % 10]`, the same rule
the Claude Code mod uses in the chat).

## Install

```sh
make build            # ./bin/cairn
make install          # go install (→ $GOPATH/bin/cairn)
cp examples/corplifting.cairn.json ~/.config/cairn/config.json
```

You need `glab` logged in to your GitLab host (`glab auth login --hostname …`).

```sh
cairn                 # run here
cairn --split         # (or `cairn split`) open in a new tmux pane, right, 33%
cairn --no-mouse      # keep the terminal's own text selection
```

## Keys

| key | action |
|---|---|
| `j` `k` / arrows | move in a list, or scroll Detail |
| `g` `G` | top / bottom |
| `1` / `2` | focus Detail / the Inbox. Pressing `2` again moves to the next tab |
| `[` `]` / click a tab title | cycle Inbox → Moje tickety → Bez ticketu (each tab keeps its own cursor) |
| `tab` `shift-tab` `h` `l` | cycle Detail → Inbox → Moje tickety → Bez ticketu |
| `enter` / click | open the item in Detail |
| `ctrl-d` `ctrl-u`, `J` `K` | scroll Detail from any section |
| `z` / `space` | collapse or expand the panel |
| `o` | open in the browser. In Detail: `o` then `N` opens image N, or a long URL shown as `↗N` |
| `y` / `Y` | copy the URL / the ID (pbcopy) |
| `c` | write a comment. `ctrl-s` asks `y/n`, then it is sent via the tracker. `esc` keeps the draft |
| `r` | refresh everything |
| `/` | filter lists (`esc` clears it) |
| `?` | help and legend |
| `q` | quit |
| mouse wheel | scroll the section under the pointer |

## Config

cairn looks for `.claude/cairn.json` in the current directory and then in each
parent. If it finds none, it uses `~/.config/cairn/config.json`. `--config FILE`
overrides both. Any field you leave out keeps its default. See
[`examples/corplifting.cairn.json`](examples/corplifting.cairn.json).

| field | meaning |
|---|---|
| `tracker.type` | `gitlab` (or `fake` for fixtures) |
| `tracker.host`, `tracker.project` | e.g. `git.applifting.cz`, `yetanothercorplifting/corplifting` |
| `workflow.boardLabels` | label substrings that mark a board column |
| `workflow.devTestStages` | columns in which "merged + deployed" means ✅ |
| `workflow.priorityLabels` | highest first |
| `workflow.bugLabel`, `epicLabelPrefix`, `hiddenLabelPrefixes` | tag handling |
| `workflow.needsMeLabels` | fallback "needs me" labels (default `Waiting on developer`) |
| `workflow.localTestPort` | 🧪 detection (`lsof` + worktree path `wt-<id>` or `/<id>`) |
| `workflow.deployRef`, `deployJob` | pipelines that deploy to dev |
| `workflow.repoPath` | local clone for `git merge-base --is-ancestor` |
| `refresh.boardSeconds` / `detailSeconds` | polling (30 / 60) |
| `bridgeDir` | default `~/.claude/cairn` |

## Architecture

```
main.go                     flags, --split, wiring
internal/config             config lookup and defaults
internal/tracker            Tracker interface, shared types, reference helpers (Mentions, Linkify)
internal/tracker/gitlab     GitLab adapter (shells out to `glab api`)
internal/tracker/fake       fixture adapter for tests and demos (never posts anywhere)
internal/adapters           picks the adapter from tracker.type
internal/workflow           flow states, ranking, tags (pure and unit-tested)
internal/bridge             reads and watches ~/.claude/cairn (fsnotify)
internal/tmux               --split
internal/ui                 Bubble Tea model, update and view
```

Network work runs only inside `tea.Cmd`s, so the UI never blocks. The header
shows the last refresh time and any errors inline (for example a 403 when
you are off the VPN).

## Adapters

The core only sees `tracker.Tracker`:

```go
Me, MyItems, Item(id) (detail + comments + images), Changes(id) (MRs/PRs),
Deployment (what dev runs, is a deploy in progress), AddComment,
URL(id), Ref(id) ("#681"), IDPattern() (finds refs in prose)
```

If image downloads need authentication, the adapter can also implement
`tracker.ImageFetcher`. To add a tracker, create
`internal/tracker/<name>`, implement the interface, and add a case in
`internal/adapters`.

- **github**: `gh api` for issues and comments (`repos/:o/:r/issues?assignee=@me`).
  Changes come from timeline cross-references or `gh pr list --search`, and
  deployment from the Deployments API or `gh run list --branch`. `IDPattern`
  is `#123`.
- **jira**: REST v3 (`/rest/api/3/search?jql=assignee=currentUser()`). Comments
  come in ADF, so convert them to markdown. Changes come from dev-status or a
  linked GitHub/GitLab. `IDPattern` is `[A-Z][A-Z0-9]+-\d+`, and ids are not
  numeric (colours fall back to a hash).
- **notion**: a database as the tracker (status property → board column).

The workflow mapping (board labels, priorities, deploy ref and job) lives in
the config, not in the adapter.

## Bridge with Claude Code

The ticket-pane mod writes to `~/.claude/cairn/`, and cairn only reads it. All
files are optional. If they are missing or only partly written, cairn shows
less but keeps working.

`state.json`:

```json
{"updatedAt": "2026-11-23T14:00:00Z",
 "active": {"kind": "ticket", "id": "681"},
 "agents": [{"id": "a1", "description": "#688 review", "status": "running",
             "step": "Read ReportDocument.cs", "tickets": ["688"]}]}
```

- `active` is `{"kind":"ticket"|"side","id":…}` or `null`. When it changes,
  cairn opens that item in Detail (a `side` item also switches to the Bez
  ticketu tab). This is how talking about #681 in the chat drives the sidebar.
  The list cursor follows too, unless you moved it yourself in the last 10 s.
- `agents[].tickets` attaches an agent to tickets. An agent named `#688 …` is
  attached by its name as well. Agents with no ticket show under *Bez ticketu*.
- Unknown fields are ignored.

`side.json` is an array of
`{"id":"B1","icon":"📝","title":"…","status":"…","url":"…","note":"…","needsYou":false}`
(`note` is markdown, `needsYou` is used only by the fallback).

`actions.json` is written by Claude when it renders `/actions`:

```json
[{"id": "#688", "icon": "🧪", "action": "① lokálně: export …"},
 {"id": "B3",   "icon": "🔀", "action": "merge !309"},
 {"id": "!313", "icon": "🔀", "action": "merge?"}]
```

An id can be a ticket (`#688`), a side item (`B3`), or an MR (`!313`, which
marks the ticket whose open MR it is). If the file is older than 24 h, cairn
ignores it and uses the heuristics.

Write both files atomically (write a `*.tmp` file, then rename it).

## Development

```sh
make check            # go vet + go test ./...
./bin/cairn --config <file with {"tracker":{"type":"fake","fixtures":"testdata/fixtures.json"}}>
```

The fake tracker logs comments to `<fixtures>.sent.log` and never posts them.

## Distribution plan

- **goreleaser** for darwin/linux, arm64/amd64. `-X main.version` is already
  wired up in the Makefile.
- A **Homebrew tap** (`brew install <tap>/cairn`).
- A **Claude Code plugin** that bundles the ticket-pane mod, which writes the
  bridge. The cairn binary is installed separately (brew or `go install`), and
  the plugin's `/cairn` command runs `cairn --split`.
