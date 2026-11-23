export type TicketImage = { alt: string; file: string; width: number; height: number }

export type TicketNote = { author: string; at: string; body: string; images: TicketImage[] }

export type TicketMr = { iid: number; state: string; mergeStatus: string; pipeline: string; branch: string }

export type Ticket = {
  iid: number
  title: string
  state: string
  labels: string[]
  assignees: string[]
  url: string
  description: string
  descriptionImages: TicketImage[]
  notes: TicketNote[]
  mrs: TicketMr[]
  fetchedAt: string
  error?: string
}

export type BoardRow = {
  iid: number
  title: string
  labels: string[]
  flow: string // our workflow state, e.g. "🤖 pracuje subagent"
  active: boolean // something is happening on it right now
  rank: number // sort key: active first, then priority
  agents: { id: string; description: string; status: string }[] // subagents working on it
}

// Work we do that has no GitLab ticket (kept by Claude in ~/.claude/ticket-pane/side-work.json).
export type SideItem = { id: string; title: string; status: string; icon?: string; url?: string; note?: string }
export type SideWork = { items: SideItem[]; agents: { id: string; description: string; status: string }[] }

declare module 'claude-code' {
  interface PluginState {
    'ticket-pane': {
      active: Shaped<number | null>
      recent: Shaped<number[]>
      board: Shaped<BoardRow[]>
      agentTickets: Shaped<Record<string, number[]>>
      agentSteps: Shaped<Record<string, string>>
      ticket: Shaped<Ticket | null>
      side: Shaped<SideWork>
      activeSide: Shaped<string | null>
      detailOpen: Shaped<boolean>
      detailOffset: Shaped<number>
    }
  }
}
