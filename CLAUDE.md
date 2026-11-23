# cairn — project rules for Claude

cairn is a terminal sidebar (Go + Bubble Tea) for ticket work, running in a
narrow tmux pane next to Claude Code. **README.md** describes what it does, the
keys, config, architecture, adapters and the bridge contract — read it first.
History and the reasons behind decisions: **docs/decisions.md**. What is next:
**docs/backlog.md**.

## Who and how

- Owner: Dominik Bláha — React developer, new to Go. Explain Go idioms briefly
  when they matter (React/Redux analogies help: Bubble Tea = Elm architecture —
  `Model` ≈ state, `Update` ≈ reducer, `View` ≈ render, `tea.Cmd` ≈ effect).
- Talk to him in **Czech**; code, comments, commits and README in **English**.
  UI strings are Czech (`Moje tickety`, `Bez ticketu`, `Na tebe teď nic nečeká ✓`).
- Numbered proposals with one global numbering, so he can answer „1 ano, 3 ne“.
- He wants to see changes running: after a UI change rebuild and restart his
  pane (see *Running and checking*), then tell him what to look at.

## Hard rules

- **Never merge, never push to a remote, never set up a remote/release** unless
  he explicitly says so. The repo has no remote yet. Commit locally, small
  commits, message style: imperative subject < 50 chars, body = why, last line
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Never post to a real tracker in tests or demos.** Comment sending is tested
  with `tracker.type: fake` (writes to `testdata/fixtures.json.sent.log`).
- The core must not know GitLab: everything tracker-specific goes through
  `internal/tracker.Tracker` and the config. Workflow mapping (labels, columns,
  priorities, deploy job, test port) lives in config, not in code.
- Design for **45–80 columns**, any height. Every layout change keeps the
  width tests (`internal/ui/ui_test.go`) green at 45, 52, 60, 80.
- The UI never blocks: network/disk work only inside `tea.Cmd`s.
- Don't touch other repos. The CorpLifting repo
  (`/Users/dominik/projects/corplifting/main`) is read-only from here — it is
  only the data source and the example config's subject.

## Running and checking

```sh
make check                      # go vet + go test ./...
make build                      # ./bin/cairn
./bin/cairn --split             # open in a tmux pane on the right, 33 %
./bin/cairn --config testdata/… # with tracker.type fake: fixtures, no network
```

Restart Dominik's running pane after a rebuild — find it, don't guess:
`tmux list-panes -a -F '#{session_name}:#{window_index}.#{pane_index} #{pane_current_command}'`
→ `tmux respawn-pane -k -t <target> $PWD/bin/cairn` (only the pane running cairn).

Visual check without disturbing him: a detached session at the target size,
then capture with colours:

```sh
tmux new-session -d -s cairn-test -x 60 -y 45 ./bin/cairn
tmux send-keys -t cairn-test j j Enter; sleep 1
tmux capture-pane -p -e -t cairn-test > /tmp/cairn-60x45.ansi
tmux kill-session -t cairn-test
```

Real data needs `glab` authenticated to git.applifting.cz and the VPN (off VPN
GitLab answers 403 — cairn must show that inline, not crash).

## The Claude Code side (bridge)

cairn reads `~/.claude/cairn/{state.json,side.json,actions.json}` (contract in
README). They are written by:

- **the Claude Code mod** in `claude-plugin/ticket-pane/` (a copy of the mod
  that was developed in another session; see decisions.md). It writes
  `state.json` (active item, subagents + their current step), renders clickable
  coloured `#ID` / `B…` links and the `/actions` table in the chat, and adds the
  `/cairn` command (focus or open the pane). It is a hooks module
  (`hooks/register.tsx`) against Claude Code's plugin API — load the
  `plugin-authoring` skill before changing it; validate with
  `claude plugin validate claude-plugin/ticket-pane`.
- **Claude itself** (in the CorpLifting session) keeps `side.json` (work without
  a ticket, ids `B1…`, never reused) and `actions.json` (everything waiting on
  Dominik, same rows as the `/actions` skill) up to date.

The mod currently loads only in the session where it was written (a per-session
dev-mods folder). Making it load everywhere is backlog item 1.

## Code map

`main.go` flags + wiring · `internal/config` lookup/defaults ·
`internal/tracker` interface, refs (`Mentions`, `Linkify`) · `tracker/gitlab`
(`glab api`) · `tracker/fake` (fixtures) · `internal/adapters` factory ·
`internal/workflow` flow states, ranking, needs-me (pure, tested) ·
`internal/bridge` read + fsnotify · `internal/tmux` `--split` ·
`internal/ui` model / update / view / detail / styles.
