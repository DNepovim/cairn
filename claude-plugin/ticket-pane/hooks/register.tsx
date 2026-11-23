import { atom, read, update } from 'claude-code'
import type { Register } from 'claude-code'

import type { BoardRow, SideWork, Ticket, TicketImage, TicketMr, TicketNote } from '../types'

const PANE = 'ticket'
const REPO = 'yetanothercorplifting%2Fcorplifting'
const WEB = 'https://git.applifting.cz/yetanothercorplifting/corplifting/-/work_items/'
const REFRESH_MS = 60_000

const active = atom({ plugin: 'ticket-pane', key: 'active' } as const, null, { shape: 'active-1' })
const ticket = atom({ plugin: 'ticket-pane', key: 'ticket' } as const, null, { shape: 'ticket-2' })
const recent = atom({ plugin: 'ticket-pane', key: 'recent' } as const, [], { shape: 'recent-1' })
const board = atom({ plugin: 'ticket-pane', key: 'board' } as const, [], { shape: 'board-2' })
const agentSteps = atom({ plugin: 'ticket-pane', key: 'agentSteps' } as const, {}, { shape: 'steps-1' })
const agentTickets = atom({ plugin: 'ticket-pane', key: 'agentTickets' } as const, {}, { shape: 'agents-1' })
const side = atom({ plugin: 'ticket-pane', key: 'side' } as const, { items: [], agents: [] } as SideWork, { shape: 'side-1' })
const activeSide = atom({ plugin: 'ticket-pane', key: 'activeSide' } as const, null as string | null, { shape: 'activeside-1' })
const detailOpen = atom({ plugin: 'ticket-pane', key: 'detailOpen' } as const, true, { shape: 'detailopen-1' })
const detailOffset = atom({ plugin: 'ticket-pane', key: 'detailOffset' } as const, 0, { shape: 'detailoffset-1' })
const CACHE = '/tmp/claude-ticket-pane'
// Links the chat rewrite points at; a press shows the item in the pane.
const SIDE_URL = 'https://corplifting.local/side/'

// "#681" and known side ids ("B3") outside code become links; a press shows them here.
function linkify(text: string, sideIds: string[]): { text: string; links: string[] } {
  const links = new Set<string>()
  const parts = text.split(/(```[\s\S]*?```|`[^`\n]*`|\[[^\]\n]*\]\([^)\n]*\))/g)
  const out = parts.map((part, i) => {
    if (i % 2 === 1) return part
    let s = part.replace(/(?<![\w/!\[])#(\d{3,4})\b/g, (_m, n: string) => {
      links.add(WEB + n)
      return `[#${n}](${WEB}${n})`
    })
    if (sideIds.length)
      s = s.replace(new RegExp(`(?<![\\w/])(${sideIds.join('|')})\\b`, 'g'), (_m, id: string) => {
        links.add(SIDE_URL + id)
        return `[${id}](${SIDE_URL}${id})`
      })
    return s
  })
  return { text: out.join(''), links: [...links] }
}
const RECENT_MAX = 8

// Ticket numbers a subagent's brief is about: "#629", "issue 629", "ticket #629".
function ticketsInBrief(text: string): number[] {
  const out: number[] = []
  for (const m of text.matchAll(/(?:#|\bissue\s+#?|\bticket\s+#?|\btiket\s+#?)(\d{3,4})\b/gi)) {
    const n = Number(m[1])
    if (!out.includes(n)) out.push(n)
  }
  return out
}

// Every ticket keeps one colour wherever it is drawn (pane and chat).
const PALETTE = ['cyan', 'magenta', 'green', 'blue', 'red', 'yellow', 'cyanBright', 'magentaBright', 'greenBright', 'blueBright'] as const
const colorOf = (iid: number) => PALETTE[iid % PALETTE.length]!

const PRIORITY: Record<string, number> = {
  'Priority: Highest': 0, 'Priority: High': 1, 'Priority: Average': 2, 'Priority: Low': 3, 'Priority: Lowest': 4,
}

async function sh($: any, script: string, ...args: string[]): Promise<string> {
  const r = await $.process.run(['sh', '-c', script, 'sh', ...args], { timeoutMs: 30_000 }).catch(() => null)
  return r && r.exitCode === 0 ? r.stdout.trim() : ''
}

// Bridge for cairn (the tmux sidebar): what only the engine knows, as one JSON
// file it watches. Written at most every 2 s; the last call always lands.
let bridgeTimer: any = null
let bridgeLast = 0
async function exportBridge($: any): Promise<void> {
  const wait = Math.max(0, 2_000 - (Date.now() - bridgeLast))
  if (bridgeTimer) return
  bridgeTimer = $.clock.after(wait, async () => {
    bridgeTimer = null
    bridgeLast = Date.now()
    try {
      const iid = await read($, active)
      const sideSel = await read($, activeSide)
      const steps = await read($, agentSteps)
      const map = await read($, agentTickets)
      const listed = ((await $.agent.list().catch(() => [])) ?? []) as { id: string; status: string; description: string }[]
      const agents = listed
        .filter(a => a.status !== 'killed')
        .slice(-30)
        .map(a => ({
          id: a.id,
          description: String(a.description ?? ''),
          status: ['completed', 'failed'].includes(a.status) ? a.status : 'running',
          step: steps[a.id] ?? '',
          tickets: [...new Set([...(map[a.id] ?? []), ...ticketsInBrief(String(a.description ?? '').split(' ')[0] ?? '')])].map(String),
        }))
      const state = {
        updatedAt: new Date().toISOString(),
        active: sideSel ? { kind: 'side', id: sideSel } : iid !== null ? { kind: 'ticket', id: String(iid) } : null,
        agents,
      }
      await sh($, 'mkdir -p "$HOME/.claude/cairn" && printf %s "$1" > "$HOME/.claude/cairn/state.json.tmp" && mv "$HOME/.claude/cairn/state.json.tmp" "$HOME/.claude/cairn/state.json"', JSON.stringify(state))
    } catch {
      // the next change writes it again
    }
  })
}

// The /actions table: "| # | | ID | Ticket — akce | Kde |". Split out so the
// chat draws it with ticket colours; everything around it stays markdown.
type ActionRow = { num: string; icon: string; id: string; title: string; action: string; whereLabel: string; whereUrl: string }
function splitActions(text: string): { before: string; rows: ActionRow[]; after: string } | null {
  const lines = text.split('\n')
  const head = lines.findIndex(l => /^\|\s*#\s*\|\s*\|\s*ID\s*\|\s*Ticket\s*—\s*akce\s*\|\s*Kde\s*\|\s*$/.test(l.trim()))
  if (head < 0 || !/^\|[-\s|:]+\|$/.test((lines[head + 1] ?? '').trim())) return null
  let end = head + 2
  const rows: ActionRow[] = []
  for (; end < lines.length && lines[end]!.trim().startsWith('|'); end++) {
    const cells = lines[end]!.trim().replace(/^\||\|$/g, '').split('|').map(c => c.trim())
    if (cells.length < 5) continue
    const [num, icon, id, body, where] = cells as [string, string, string, string, string]
    const at = body.indexOf(' — ')
    const link = where.match(/\[([^\]]*)\]\(([^)]+)\)/)
    rows.push({
      num, icon, id: id.replace(/[`*]/g, ''),
      title: at >= 0 ? body.slice(0, at) : body,
      action: at >= 0 ? body.slice(at + 3) : '',
      whereLabel: link ? link[1]! : where,
      whereUrl: link ? link[2]! : '',
    })
  }
  return { before: lines.slice(0, head).join('\n'), rows, after: lines.slice(end).join('\n') }
}

// Work without a ticket: the list Claude keeps in a file, plus subagents whose
// name carries no "#ID" prefix.
async function refreshSide($: any): Promise<void> {
  const raw = await sh($, 'cat "$HOME/.claude/cairn/side.json" 2>/dev/null')
  let items: SideWork['items'] = []
  try {
    const parsed = JSON.parse(raw || '[]')
    if (Array.isArray(parsed)) items = parsed.filter(i => i && typeof i.title === 'string' && typeof i.id === 'string')
  } catch {
    items = (await read($, side)).items
  }
  const listed = (await $.agent.list().catch(() => null)) as { id: string; status: string; description: string }[] | null
  const map = await read($, agentTickets)
  const agents =
    listed === null
      ? (await read($, side)).agents
      : listed
          .filter(a => !['completed', 'failed', 'killed'].includes(a.status))
          .filter(a => (map[a.id] ?? []).length === 0 && ticketsInBrief(String(a.description ?? '').split(' ')[0] ?? '').length === 0)
          .map(a => ({ id: a.id, description: String(a.description ?? ''), status: 'running' }))
  await update($, side, () => ({ items, agents }))
}

// The "my tickets" list: GitLab labels plus where our own workflow stands.
async function refreshBoard($: any): Promise<void> {
  void refreshSide($)
  void exportBridge($)
  try {
    const me = String(((await glab($, 'user')) as any).username)
    const issues = (await glab($, `projects/${REPO}/issues?assignee_username=${me}&state=opened&per_page=100`)) as any[]
    const pipes = (await glab($, `projects/${REPO}/pipelines?ref=develop&per_page=5`)) as any[]
    const deployed = pipes.find(p => p.status === 'success')?.sha as string | undefined
    const deploying = pipes[0] && ['running', 'pending', 'created'].includes(String(pipes[0].status))
    // Which worktree serves :5013 right now (the local approval run).
    const served = await sh($, 'p=$(lsof -nP -iTCP:5013 -sTCP:LISTEN -t 2>/dev/null | head -1); [ -n "$p" ] && ps -o command= -p "$p"')
    const running = new Set<number>()
    const byTicket = new Map<number, { id: string; description: string; status: string }[]>()
    const map = await read($, agentTickets)
    // A failed listing keeps the agents from the last good one instead of blanking them.
    const listed = (await $.agent.list().catch(() => null)) as { id: string; status: string; description: string }[] | null
    const previous = await read($, board)
    if (listed === null) for (const r of previous) if ((r.agents ?? []).length) byTicket.set(r.iid, r.agents)
    for (const a of listed ?? []) {
      // A ticket is known from the brief (agent.spawn) or from the "#681 …" name prefix.
      const ids = new Set([...(map[a.id] ?? []), ...ticketsInBrief(String(a.description ?? '').split(' ')[0] ?? '')])
      // Only a finished agent is done; idle/waiting between rounds still counts as working.
      const done = ['completed', 'failed', 'killed'].includes(a.status)
      for (const n of ids) {
        if (!done) running.add(n)
        if (a.status !== 'killed')
          byTicket.set(n, [...(byTicket.get(n) ?? []), { id: a.id, description: String(a.description ?? ''), status: done ? a.status : 'running' }])
      }
    }

    const rows: BoardRow[] = []
    for (const i of issues) {
      const iid = Number(i.iid)
      const labels = (i.labels ?? []) as string[]
      const mrs = (await glab($, `projects/${REPO}/issues/${iid}/related_merge_requests`).catch(() => [])) as any[]
      const open = mrs.filter(m => m.state === 'opened')
      const merged = mrs.filter(m => m.state === 'merged' && m.merge_commit_sha)
      const newest = merged.sort((a, b) => String(b.merged_at).localeCompare(String(a.merged_at)))[0]
      let onDev = false
      if (newest && deployed)
        onDev = (await sh($, 'git merge-base --is-ancestor "$1" "$2" && echo yes', newest.merge_commit_sha, deployed)) === 'yes'
      const stage = labels.find(l => /Waiting on developer|Developing|Code review|Ready for|Backlog/.test(l)) ?? ''
      let flow = ''
      let active = true
      if (running.has(iid)) flow = '🤖 pracuje subagent'
      else if (served && new RegExp(`(wt-|/)${iid}[^0-9]`).test(served)) flow = '🧪 připraveno k lokálnímu testu'
      else if (open.length > 0)
        flow = `📝 MR !${open[0].iid} otevřený${open[0].head_pipeline?.status === 'failed' ? ' ✗ pipeline' : ''}`
      else if (newest && !onDev && deploying) flow = '🚀 nasazuje se na dev'
      else if (newest && onDev && /Waiting on developer|Code review|Developing/.test(stage)) flow = '✅ připraveno k testu na devu'
      else {
        active = false
        flow = stage ? `⏸ ${stage}` : '—'
      }
      const prio = PRIORITY[labels.find(l => l in PRIORITY) ?? ''] ?? 5
      const agentsHere = (byTicket.get(iid) ?? []).slice(-6)
      rows.push({ iid, title: String(i.title ?? ''), labels, flow, active, rank: (active ? 0 : 100) + prio * 10 + (labels.includes('Bug') ? 0 : 1), agents: agentsHere })
    }
    rows.sort((a, b) => a.rank - b.rank || b.iid - a.iid)
    await update($, board, () => rows)
  } catch {
    // keep the last list; the next tick tries again
  }
}

// Ticket numbers mentioned in a text: "#629" (3–4 digits), in order, distinct.
function mentions(text: string): number[] {
  const seen: number[] = []
  for (const m of text.matchAll(/(?<![\w/!])#(\d{3,4})\b/g)) {
    const n = Number(m[1])
    if (!seen.includes(n)) seen.push(n)
  }
  return seen
}

async function remember($: any, ids: number[]): Promise<void> {
  if (ids.length === 0) return
  await update($, recent, list => [...ids, ...list.filter(n => !ids.includes(n))].slice(0, RECENT_MAX))
}

// Which issue a tool call is about. Only literal numbers count, so loops over
// many issues ("issues/$id") never move the pane.
const SKILLS = new Set(['ticket', 'spec', 'build', 'ship', 'verify'])
function issueFromCall(e: Record<string, unknown>): number | null {
  const tool = String(e.tool ?? '')
  if (tool === 'Bash') {
    const cmd = String(e.command ?? '')
    const m =
      cmd.match(/\bglab issue (?:view|update|note)\s+#?(\d+)/) ??
      cmd.match(/\/issues\/(\d+)(?:[/"'\s?]|$)/) ??
      cmd.match(/(?:implement|commit)-issue\.sh\s+(\d+)/)
    return m ? Number(m[1]) : null
  }
  if (tool === 'mcp__gitlab-applifting__get_work_item' || tool === 'mcp__gitlab-applifting__save_work_item') {
    const iid = Number(e.work_item_iid ?? e.iid)
    return Number.isFinite(iid) && iid > 0 ? iid : null
  }
  if (tool === 'Skill' && SKILLS.has(String(e.skill ?? ''))) {
    const m = String(e.args ?? '').match(/#?(\d+)/)
    return m ? Number(m[1]) : null
  }
  return null
}

// GitLab image uploads are relative links the terminal can't follow; the
// markdown keeps a numbered marker and the picture is drawn under the block.
const UPLOAD = /!\[([^\]]*)\]\(\/uploads\/([0-9a-f]{16,64})\/([^)\s]+)\)(\{[^}]*\})?/g
const tidy = (md: string) => {
  let n = 0
  return md
    .replace(UPLOAD, (_m, alt: string) => `[obrázek ${++n}: ${alt}]`)
    .replace(/!\[([^\]]*)\]\([^)]*\)(\{[^}]*\})?/g, '[obrázek: $1]')
    .trim()
}

const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'
function bytesOf(b64: string, count: number): number[] {
  const out: number[] = []
  let buf = 0
  let bits = 0
  for (const ch of b64) {
    const v = B64.indexOf(ch)
    if (v < 0) continue
    buf = (buf << 6) | v
    bits += 6
    if (bits >= 8) {
      bits -= 8
      out.push((buf >> bits) & 0xff)
      if (out.length >= count) break
    }
  }
  return out
}

// Downloads an upload once (as PNG; sips converts JPEG/GIF on macOS) and
// reads its size from the IHDR header.
async function fetchImage($: any, secret: string, name: string, alt: string): Promise<TicketImage | null> {
  const safe = `${secret}-${name.replace(/[^A-Za-z0-9._-]/g, '_')}`.replace(/\.[^.]*$/, '') + '-s.png'
  const file = `${CACHE}/${safe}`
  // Files under /tmp are read through the shell: $.fs is held to the session's
  // folders. The first 24 bytes (PNG signature + IHDR) give the size.
  const r = await $.process.run(
    [
      'sh', '-c',
      '[ -s "$5" ] || { mkdir -p "$1" && glab api "projects/$2/uploads/$3/$4" > "$5.tmp" && ' +
        '(sips -s format png -Z 1000 "$5.tmp" --out "$5" >/dev/null 2>&1 || mv "$5.tmp" "$5"); rm -f "$5.tmp"; } ; ' +
        'head -c 24 "$5" | base64',
      'sh', CACHE, REPO, secret, name, file,
    ],
    { timeoutMs: 30_000 },
  )
  if (r.exitCode !== 0) return null
  const b = bytesOf(r.stdout.trim(), 24)
  if (b.length < 24 || b[1] !== 0x50 || b[2] !== 0x4e || b[3] !== 0x47) return null
  const width = ((b[16]! << 24) | (b[17]! << 16) | (b[18]! << 8) | b[19]!) >>> 0
  const height = ((b[20]! << 24) | (b[21]! << 16) | (b[22]! << 8) | b[23]!) >>> 0
  return width > 0 && height > 0 ? { alt, file, width, height } : null
}

async function imagesOf($: any, md: string): Promise<TicketImage[]> {
  const found = [...md.matchAll(UPLOAD)]
  const imgs = await Promise.all(found.map(m => fetchImage($, m[2]!, m[3]!, m[1] ?? '')))
  return imgs.filter((i): i is TicketImage => i !== null)
}

async function glab($: any, path: string): Promise<unknown> {
  const r = await $.process.run(['glab', 'api', path], { timeoutMs: 20_000 })
  if (r.exitCode !== 0) throw new Error(r.stderr.trim() || `glab exited ${r.exitCode}`)
  return JSON.parse(r.stdout)
}

async function refresh($: any): Promise<void> {
  const iid = await read($, active)
  if (iid === null) return
  try {
    const [issue, notes, mrs] = (await Promise.all([
      glab($, `projects/${REPO}/issues/${iid}`),
      glab($, `projects/${REPO}/issues/${iid}/notes?sort=asc&per_page=100`),
      glab($, `projects/${REPO}/issues/${iid}/related_merge_requests`),
    ])) as [any, any[], any[]]
    const humanNotes = notes.filter(n => !n.system)
    const [descriptionImages, noteImages] = await Promise.all([
      imagesOf($, String(issue.description ?? '')),
      Promise.all(humanNotes.map(n => imagesOf($, String(n.body ?? '')))),
    ])
    const next: Ticket = {
      iid,
      title: String(issue.title ?? ''),
      state: String(issue.state ?? ''),
      labels: (issue.labels ?? []) as string[],
      assignees: ((issue.assignees ?? []) as any[]).map(a => String(a.username)),
      url: WEB + iid,
      description: tidy(String(issue.description ?? '')),
      descriptionImages,
      notes: humanNotes.map((n, i): TicketNote => ({
        author: String(n.author?.username ?? '?'),
        at: String(n.created_at ?? '').slice(0, 16).replace('T', ' '),
        body: tidy(String(n.body ?? '')),
        images: noteImages[i] ?? [],
      })),
      mrs: mrs.map((m): TicketMr => ({
        iid: Number(m.iid),
        state: String(m.state),
        mergeStatus: String(m.detailed_merge_status ?? ''),
        pipeline: String(m.head_pipeline?.status ?? '-'),
        branch: String(m.source_branch ?? ''),
      })),
      fetchedAt: new Date().toISOString().slice(11, 16),
    }
    if ((await read($, active)) === iid) await update($, ticket, () => next)
  } catch (err) {
    await update($, ticket, t => ({
      ...(t && t.iid === iid ? t : { iid, title: '', state: '', labels: [], assignees: [], url: WEB + iid, description: '', descriptionImages: [], notes: [], mrs: [], fetchedAt: '' }),
      error: String((err as Error).message ?? err).slice(0, 300),
    }))
  }
}

async function showSide($: any, id: string): Promise<void> {
  await update($, detailOpen, () => true)
  await update($, detailOffset, () => 0)
  await update($, activeSide, () => id)
  void exportBridge($)
}

async function show($: any, iid: number): Promise<void> {
  await update($, detailOpen, () => true)
  if ((await read($, active)) !== iid || (await read($, activeSide)) !== null) await update($, detailOffset, () => 0)
  await update($, activeSide, () => null)
  if ((await read($, active)) !== iid) {
    await update($, active, () => iid)
    await update($, ticket, () => null)
  }
  await remember($, [iid])
  void exportBridge($)
  await refresh($)
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'cairn',
      description: 'Open cairn (the ticket sidebar) in a tmux pane, or focus it: /cairn 646 selects a ticket',
      argumentHint: '[issue number]',
    })
    $.clock.every(REFRESH_MS, () => void refresh($))
    $.clock.every(30_000, () => void refreshBoard($))
    void refreshBoard($)
    // The pane moved to cairn; close it where an older version left it open.
    void Promise.resolve($.ui.close({ id: PANE })).catch(() => undefined)
    return next(e)
  })

  // Remember which tickets a subagent works on, from its brief.
  on('agent.spawn', async ($, e, next) => {
    const r = await next(e)
    // Only the "#681 …" name prefix counts; a brief may mention other tickets as context.
    const ids = ticketsInBrief(String((e as any).description ?? '').split(' ')[0] ?? '')
    const agentId = (r as any)?.agentId as string | undefined
    if (agentId && ids.length > 0) {
      await update($, agentTickets, m => ({ ...m, [agentId]: ids }))
      void refreshBoard($)
    }
    return r
  })

  on('command.run', { command: 'cairn' }, async ($, e) => {
    const m = String((e as any).args ?? '').match(/\d+/)
    if (m) await show($, Number(m[0]))
    // Focus the cairn pane of this window, or split one off at a third of its width.
    const out = await sh(
      $,
      'p=$(tmux list-panes -F "#{pane_id} #{pane_current_command}" 2>/dev/null | awk \'$2=="cairn"{print $1; exit}\'); ' +
        'if [ -n "$p" ]; then tmux select-pane -t "$p" && echo focused; ' +
        'elif [ -n "$TMUX" ]; then "$HOME/projects/cairn/bin/cairn" --split >/dev/null 2>&1 && echo opened; ' +
        'else echo notmux; fi',
    )
    return { text: out === 'focused' ? 'cairn: přepnuto do panelu.' : out === 'opened' ? 'cairn: otevřen vpravo.' : 'cairn: běží mimo tmux – spusť `cairn` v terminálu.' }
  })

  on('tool.call', async ($, e, next) => {
    const call = e as unknown as Record<string, unknown>
    // A subagent's current step, as the line under the prompt shows it.
    const agentId = call.agentId as string | undefined
    if (agentId) {
      const step = String(call.description ?? '') || `${String(call.tool ?? '')} ${String(call.command ?? call.file_path ?? call.pattern ?? call.url ?? '').slice(0, 60)}`.trim()
      void update($, agentSteps, m => ({ ...m, [agentId]: step.slice(0, 90) }))
      void exportBridge($)
    }
    const iid = issueFromCall(call)
    const ran = await next(e)
    if (iid !== null) await show($, iid)
    return ran
  })

  // Claude's own replies in the main conversation: one ticket talked about →
  // switch to it; several → offer them as buttons in the pane.
  on('session.append', async ($, e, next) => {
    const done = await next(e)
    const msg = e.message
    if (e.agentId === undefined && msg.type === 'assistant' && !msg.isMeta) {
      const text = msg.content
        .map(b => ((b as { type?: string; text?: string }).type === 'text' ? String((b as { text?: string }).text ?? '') : ''))
        .join('\n')
      const ids = mentions(text)
      if (ids.length === 1) await show($, ids[0]!)
      else await remember($, ids)
    }
    return done
  })

  on('prompt.submit', async ($, e, next) => {
    const m = String(e.text ?? '').match(/(?:#|\bticket\s+|\btiket\s+)(\d{3,4})\b/i)
    if (m) void show($, Number(m[1]))
    return next(e)
  })

  // In the chat: a reply about exactly one ticket gets a coloured "▌ #681 Title"
  // line above it; the reply itself is drawn by the engine as usual.
  on('ui.render', { component: 'AssistantMessage' }, async ($, e, next) => {
    const p = e.props as { text?: string; isFirstOfReply?: boolean; isSummary?: true }
    if (p.isSummary) return next(e)
    const raw = String(p.text ?? '')
    const sideIds = (await read($, side)).items.map(i => i.id).filter(id => /^[A-Z]\d{1,3}$/.test(id))
    const table = splitActions(raw)
    const { text, links } = linkify(table ? table.before : raw, sideIds)
    const afterLinked = table ? linkify(table.after, sideIds) : { text: '', links: [] as string[] }
    const ids = mentions(table ? table.before + '\n' + table.after : raw)
    if (links.length === 0 && !table) return next(e)
    const { Box, Text, Markdown } = $.ui.resolve(e)
    const press = (link: { href: string }) => {
      const href = String(link.href)
      if (href.startsWith(SIDE_URL)) void showSide($, href.slice(SIDE_URL.length))
      else if (href.startsWith(WEB)) void show($, Number(href.slice(WEB.length)))
    }
    let header: any = null
    if (p.isFirstOfReply && ids.length === 1) {
      const iid = ids[0]!
      const rows = await read($, board)
      const t = await read($, ticket)
      const title = rows.find(r => r.iid === iid)?.title ?? (t?.iid === iid ? t.title : '')
      header = <Text bold color={colorOf(iid)}>{`▌ #${iid}${title ? ` ${title}` : ''}`}</Text>
    }
    // Markdown cannot colour a link, so the ids it mentions repeat below the block
    // in their pane colours, each a button that shows it in the pane.
    const els = $.ui.resolve(e)
    const Button = 'Button' in els ? (els as any).Button : undefined
    const sideHere = sideIds.filter(id => links.includes(SIDE_URL + id))
    const chips =
      Button && ids.length + sideHere.length > 1 ? (
        <Box flexDirection="row" flexWrap="wrap" gap={1}>
          <Text dimColor>↳</Text>
          {ids.map(n => (
            <Button key={`chip${n}`} plain onPress={() => void show($, n)}>
              <Text bold color={colorOf(n)}>{`#${n}`}</Text>
            </Button>
          ))}
          {sideHere.map(id => (
            <Button key={`chip${id}`} plain onPress={() => void showSide($, id)}>
              <Text bold color="yellow">{id}</Text>
            </Button>
          ))}
        </Box>
      ) : null
    const idColor = (id: string) => (/^#\d+$/.test(id) ? colorOf(Number(id.slice(1))) : /^B\d+$/.test(id) ? 'yellow' : 'magenta')
    const openId = (id: string) => {
      if (/^#\d+$/.test(id)) void show($, Number(id.slice(1)))
      else if (/^B\d+$/.test(id)) void showSide($, id)
      else if (/^!\d+$/.test(id)) void $.process.run(['open', `https://git.applifting.cz/yetanothercorplifting/corplifting/-/merge_requests/${id.slice(1)}`])
    }
    const actionTable = table && Button ? (
      <Box flexDirection="column" marginY={1}>
        {table.rows.map((r, n) => (
          <Box key={`act${n}`} flexDirection="row" marginBottom={1}>
            <Box width={3}><Text dimColor>{r.num}</Text></Box>
            <Box width={3}><Text>{r.icon}</Text></Box>
            <Box width={7}>
              <Button key={`actid${n}`} plain onPress={() => openId(r.id)}>
                <Text bold color={idColor(r.id)}>{r.id}</Text>
              </Button>
            </Box>
            <Box flexDirection="column" flexGrow={1} flexShrink={1}>
              <Text bold color={idColor(r.id)}>{r.title}</Text>
              {r.action && <Text>{r.action}</Text>}
            </Box>
            {r.whereUrl ? (
              <Box marginLeft={1}>
                <Button key={`actwhere${n}`} plain onPress={() => void $.process.run(['open', r.whereUrl])}>
                  <Text color="blue" underline>{`${r.whereLabel} ↗`}</Text>
                </Button>
              </Box>
            ) : null}
          </Box>
        ))}
      </Box>
    ) : table ? (
      <Markdown key="acttable" text={raw.split('\n').slice(table.before.split('\n').length, raw.split('\n').length - (table.after ? table.after.split('\n').length : 0)).join('\n')} />
    ) : null
    const allLinks = [...links, ...afterLinked.links]
    return (
      <Box flexDirection="column">
        {header}
        {text.trim() && <Markdown key="reply" text={text} onLinkPress={press} pressableLinks={links} />}
        {actionTable}
        {table && afterLinked.text.trim() && <Markdown key="reply-after" text={afterLinked.text} onLinkPress={press} pressableLinks={afterLinked.links} />}
        {allLinks.length ? chips : null}
      </Box>
    )
  })
}

// Posts a comment the person typed in the pane, as them (glab's own login).
async function postComment($: any, iid: number, text: string): Promise<void> {
  const body = text.trim()
  if (!body) return
  const r = await $.process.run(
    ['glab', 'issue', 'note', String(iid), '--repo', 'yetanothercorplifting/corplifting', '-m', body],
    { timeoutMs: 30_000 },
  )
  $.ui.toast(r.exitCode === 0 ? `Komentář přidán k #${iid}` : `Komentář se nepodařilo odeslat: ${r.stderr.trim().slice(0, 200)}`)
  await refresh($)
}
