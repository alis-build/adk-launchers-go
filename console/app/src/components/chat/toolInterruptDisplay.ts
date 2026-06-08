/**
 * Display helpers for tool confirmation interrupts.
 *
 * Extracts human-readable titles, risk tiers, and status hints from
 * AG-UI {@link Interrupt} metadata for use in permission cards and
 * batch checklists.
 *
 * @module components/chat/toolInterruptDisplay
 */
import type { ChatToolCall, Interrupt } from '@/pages/threads/types'

/** Categorises the risk level of a tool call requiring confirmation. */
export type InterruptRiskTier = 'read' | 'write' | 'send'

/** HITL metadata attached to an interrupt by the agent launcher. */
interface HitlMetadata {
  risk?: string
  summary?: string
}

/** Extracts the `hitl` metadata bag from an interrupt, if present. */
function hitlMeta(interrupt: Interrupt): HitlMetadata | undefined {
  const meta = interrupt.metadata as Record<string, unknown> | undefined
  const hitl = meta?.hitl
  if (!hitl || typeof hitl !== 'object') return undefined
  return hitl as HitlMetadata
}

/** Compact inline preview of tool arguments for cards and checklist rows. */
export function formatArgsInline(args: Record<string, unknown> | undefined): string {
  if (!args || Object.keys(args).length === 0) return ''
  return Object.entries(args)
    .map(([k, v]) => `${k}: ${JSON.stringify(v)}`)
    .join(', ')
}

/** Human-readable title for a tool confirmation interrupt. */
export function getInterruptTitle(interrupt: Interrupt, toolCall?: ChatToolCall): string {
  if (interrupt.message?.trim()) return interrupt.message.trim()
  const summary = hitlMeta(interrupt)?.summary
  if (typeof summary === 'string' && summary.trim()) return summary.trim()
  const name = toolCall?.name ?? 'tool'
  return `Agent wants to run ${name}`
}

/** Subtitle line: tool name plus optional server summary from metadata. */
export function getInterruptSubtitle(interrupt: Interrupt, toolCall?: ChatToolCall): string {
  const parts: string[] = []
  if (toolCall?.name) parts.push(`tool ${toolCall.name}`)
  const summary = hitlMeta(interrupt)?.summary
  if (summary && summary !== interrupt.message) {
    parts.push(summary)
  }
  return parts.join(' · ')
}

/** Resolves the risk tier from `hitl.risk` or `adk.risk` metadata. */
export function getInterruptRiskTier(interrupt: Interrupt): InterruptRiskTier | undefined {
  const risk = hitlMeta(interrupt)?.risk
  if (risk === 'read' || risk === 'write' || risk === 'send') return risk
  const adk = (interrupt.metadata as Record<string, unknown> | undefined)?.adk as Record<string, unknown> | undefined
  const adkRisk = adk?.risk
  if (adkRisk === 'read' || adkRisk === 'write' || adkRisk === 'send') return adkRisk
  return undefined
}

/** User-facing label for a risk tier chip (e.g. "READ-ONLY"). */
export function riskTierLabel(tier: InterruptRiskTier): string {
  switch (tier) {
    case 'read': return 'READ-ONLY'
    case 'write': return 'WRITES DATA'
    case 'send': return 'SENDS EMAIL'
  }
}

/** True when batch footer should use "Review & run" label. */
export function footerNeedsReviewLabel(interrupts: Interrupt[]): boolean {
  return interrupts.some((intr) => {
    const tier = getInterruptRiskTier(intr)
    return tier === 'write' || tier === 'send'
  })
}

/** Submit button label for the batch checklist footer. */
export function batchFooterLabel(runCount: number, interrupts: Interrupt[]): string {
  if (runCount <= 0) return 'Submit responses'
  if (footerNeedsReviewLabel(interrupts)) {
    return `Review & run (${runCount})`
  }
  return runCount === 1 ? 'Run 1 action' : `Run ${runCount} actions`
}

/** Short hint shown next to the status indicator when confirmation is pending. */
export function statusHintForInterrupts(count: number): string {
  if (count <= 1) return 'Needs your OK to continue'
  return `${count} actions need your OK`
}

/** ADK invocation id from interrupt metadata (for confirmation resume). */
export function getAdkInvocationId(interrupt: Interrupt): string | undefined {
  const adk = interrupt.metadata?.adk as Record<string, unknown> | undefined
  const id = adk?.invocationId
  return typeof id === 'string' && id ? id : undefined
}
