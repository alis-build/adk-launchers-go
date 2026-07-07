/**
 * Minimal JSON-RPC 2.0 client for same-origin console backend services.
 *
 * @module utils/jsonrpc
 */

export const HISTORY_JSONRPC_PATH = '/alis.agui.history.v1.ThreadService'
export const SCHEDULER_JSONRPC_PATH = '/alis.agui.scheduler.v1.SchedulerService'

export interface JsonRpcError {
  code: number
  message: string
  data?: unknown
}

export class JsonRpcClientError extends Error {
  readonly code: number
  readonly data?: unknown

  constructor(error: JsonRpcError) {
    super(error.message)
    this.name = 'JsonRpcClientError'
    this.code = error.code
    this.data = error.data
  }
}

let requestId = 0

/**
 * Posts a JSON-RPC 2.0 request and returns the `result` field.
 *
 * Params are sent as protojson-compatible camelCase objects.
 */
export async function postJsonRpc<T>(url: string, method: string, params?: Record<string, unknown>): Promise<T> {
  const id = ++requestId
  const response = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      jsonrpc: '2.0',
      method,
      params: params ?? {},
      id,
    }),
  })

  if (!response.ok) {
    throw new Error(`JSON-RPC HTTP ${response.status}: ${response.statusText}`)
  }

  const payload = (await response.json()) as {
    result?: T
    error?: JsonRpcError
  }

  if (payload.error) {
    throw new JsonRpcClientError(payload.error)
  }

  return payload.result as T
}
