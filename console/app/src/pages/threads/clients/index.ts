/**
 * AG-UI client for chat and thread HTTP/JSON-RPC APIs.
 *
 * @module pages/threads/clients
 */
import { HISTORY_JSONRPC_PATH, postJsonRpc } from '@/utils/jsonrpc'
import { ADKAgent } from '@ag-ui/adk'

/**
 * Creates an AG-UI ADKAgent for streaming chat with the agent.
 */
export function createAguiAgent(threadId: string, headers?: Record<string, string>): ADKAgent {
  return new ADKAgent({
    url: '/agui/run_sse',
    threadId,
    headers,
    fetch: (url, init) => fetch(url, init),
  })
}

export interface UserThreadState {
  thread?: string
  readRunCount?: number | string
  pinned?: boolean
  pinnedTime?: { seconds?: number | string; nanos?: number }
}

export interface UpdateUserThreadStateRequest {
  userThreadState: {
    thread: string
    readRunCount?: number | string
    pinned?: boolean
  }
  /** protojson FieldMask: comma-separated snake_case paths (e.g. "pinned", "read_run_count"). */
  updateMask: string
}

/** Updates per-user thread state (read count, pinned) via history JSON-RPC. */
export async function updateUserThreadState(req: UpdateUserThreadStateRequest): Promise<UserThreadState> {
  const params = {
    userThreadState: {
      ...req.userThreadState,
      readRunCount: req.userThreadState.readRunCount,
    },
    updateMask: req.updateMask,
  }
  return postJsonRpc<UserThreadState>(HISTORY_JSONRPC_PATH, 'UpdateUserThreadState', params)
}

/** An AG-UI Message as returned by the thread messages endpoint. */
export interface AGUIMessage {
  id: string
  role: string
  content?: unknown
  toolCalls?: Array<{ id: string; type: string; function: { name: string; arguments: string } }>
  toolCallId?: string
  activityType?: string
  name?: string
}

interface ThreadMessagesResponse {
  messages: AGUIMessage[]
  nextCursor?: string
}

/**
 * Fetches thread message history from the AG-UI messages endpoint.
 */
export async function fetchThreadMessages(
  threadId: string,
  opts?: { after?: string; limit?: number; agentId?: string },
): Promise<ThreadMessagesResponse> {
  const params = new URLSearchParams()
  if (opts?.after) params.set('after', opts.after)
  if (opts?.limit) params.set('limit', String(opts.limit))
  if (opts?.agentId) params.set('agentId', opts.agentId)
  const query = params.toString()
  const url = `/agui/threads/${encodeURIComponent(threadId)}/messages${query ? `?${query}` : ''}`

  const res = await fetch(url, {
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) {
    throw new Error(`Failed to fetch thread messages: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

interface FetchThreadsOptions {
  agentId?: string
  pageSize?: number
  pageToken?: string
}

interface FetchThreadsResponse {
  threads: unknown[]
  nextPageToken?: string
}

export async function fetchThread(threadId: string): Promise<Record<string, unknown>> {
  const url = `/agui/threads/${encodeURIComponent(threadId)}`
  const res = await fetch(url, {
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) {
    throw new Error(`Failed to fetch thread: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function deleteThread(threadId: string): Promise<void> {
  const url = `/agui/threads/${encodeURIComponent(threadId)}`
  const res = await fetch(url, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`Failed to delete thread: ${res.status} ${res.statusText}`)
  }
}

export async function fetchThreads(opts?: FetchThreadsOptions): Promise<FetchThreadsResponse> {
  const params = new URLSearchParams()
  if (opts?.agentId) params.set('agentId', opts.agentId)
  if (opts?.pageSize) params.set('pageSize', String(opts.pageSize))
  if (opts?.pageToken) params.set('pageToken', opts.pageToken)
  const query = params.toString()
  const url = `/agui/threads${query ? `?${query}` : ''}`

  const res = await fetch(url, {
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) {
    throw new Error(`Failed to fetch threads: ${res.status} ${res.statusText}`)
  }
  return res.json()
}
