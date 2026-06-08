/**
 * Automation scheduler JSON-RPC client.
 *
 * @module pages/automation/client
 */
import { JsonRpcClientError, postJsonRpc, SCHEDULER_JSONRPC_PATH } from '@/utils/jsonrpc'

export interface TimestampInput {
  seconds?: bigint | number | string
  nanos?: number
}

/** Timestamp from API responses: protojson RFC 3339 string or legacy object shape. */
export type TimestampLike = string | TimestampInput

export interface Cron {
  name: string
  prompt?: string
  initialPrompt?: string
  expr?: string
  timezone?: string
  type?: number
  owner?: string
  email?: string
  at?: TimestampLike
  createTime?: TimestampLike
  updateTime?: TimestampLike
  lastRunTime?: TimestampLike
  archiveTime?: TimestampLike
}

/** Cron schedule type values (matches scheduler proto). */
export const Cron_Type = {
  CRON: 1,
  AT: 2,
} as const

export type CronType = (typeof Cron_Type)[keyof typeof Cron_Type]

export async function listCrons(params: { pageSize?: number; pageToken?: string } = {}): Promise<{ crons?: Cron[]; nextPageToken?: string }> {
  return postJsonRpc(SCHEDULER_JSONRPC_PATH, 'ListCrons', params)
}

/** Converts a timestamp to protojson RFC 3339 string for JSON-RPC request params. */
export function toProtojsonTimestamp(ts?: TimestampInput): string | undefined {
  if (!ts) return undefined

  const seconds = typeof ts.seconds === 'bigint' ? Number(ts.seconds) : Number(ts.seconds ?? 0)
  const nanos = ts.nanos ?? 0
  const date = new Date(seconds * 1000 + Math.floor(nanos / 1_000_000))
  if (Number.isNaN(date.getTime())) return undefined

  return date.toISOString()
}

/** Parses protojson timestamp strings or {seconds,nanos} objects into a Date. */
export function parseTimestampLike(ts?: TimestampLike): Date | null {
  if (!ts) return null

  if (typeof ts === 'string') {
    const date = new Date(ts)
    return Number.isNaN(date.getTime()) ? null : date
  }

  const seconds = typeof ts.seconds === 'bigint' ? Number(ts.seconds) : Number(ts.seconds ?? 0)
  const nanos = ts.nanos ?? 0
  const date = new Date(seconds * 1000 + Math.floor(nanos / 1_000_000))
  return Number.isNaN(date.getTime()) ? null : date
}

export function timestampLikeToMs(ts?: TimestampLike): number {
  const date = parseTimestampLike(ts)
  return date?.getTime() ?? 0
}

export function formatTimestampLike(ts?: TimestampLike): string {
  const date = parseTimestampLike(ts)
  if (!date) return 'Unknown'

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

export async function createCron(params: { cron: Partial<Cron> }): Promise<Cron> {
  const { at, ...rest } = params.cron
  const cron: Record<string, unknown> = { ...rest }

  const atValue = toProtojsonTimestamp(typeof at === 'string' ? undefined : at)
  if (atValue) {
    cron.at = atValue
  }

  return postJsonRpc<Cron>(SCHEDULER_JSONRPC_PATH, 'CreateCron', { cron })
}

export async function deleteCron(params: { name: string }): Promise<Record<string, never>> {
  return postJsonRpc(SCHEDULER_JSONRPC_PATH, 'DeleteCron', params)
}

export async function runCron(params: { id: string }): Promise<Record<string, never>> {
  return postJsonRpc(SCHEDULER_JSONRPC_PATH, 'RunCron', params)
}

/**
 * Extracts a human-readable error message from a scheduler client error.
 */
export function formatSchedulerClientError(err: unknown, fallback: string): string {
  if (err instanceof JsonRpcClientError) {
    return err.message || fallback
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}
