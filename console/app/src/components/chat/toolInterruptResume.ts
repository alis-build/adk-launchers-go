/**
 * Builds AG-UI {@link ResumeEntry} arrays from user confirmation decisions.
 *
 * @module components/chat/toolInterruptResume
 */
import type { Interrupt, ResumeEntry, ToolConfirmationPayload } from '@/pages/threads/types'

/** Per-interrupt user input collected by the batch checklist. */
export interface InterruptUserInputState {
  payload?: ToolConfirmationPayload
  cancelled?: boolean
}

/** Plain copy for ADKAgent.prepareRunAgentInput (uses structuredClone on resume). */
function toPlainPayload(payload: ToolConfirmationPayload): ToolConfirmationPayload {
  return JSON.parse(JSON.stringify(payload)) as ToolConfirmationPayload
}

/** Single-interrupt resume entry (Allow & run / Deny). */
export function buildSingleResumeEntry(
  interruptId: string,
  payload: ToolConfirmationPayload,
): ResumeEntry[] {
  return [{
    interruptId,
    status: 'resolved',
    payload: toPlainPayload(payload),
  }]
}

/** Builds AG-UI resume entries from per-interrupt user input (approved / editedArgs, not legacy Confirmed). */
export function buildInterruptResumeEntries(
  interrupts: Interrupt[],
  userInputs: Record<string, InterruptUserInputState>,
): ResumeEntry[] {
  return interrupts.map((intr) => {
    const input = userInputs[intr.id]
    if (input?.cancelled) {
      return { interruptId: intr.id, status: 'cancelled' as const }
    }
    return {
      interruptId: intr.id,
      status: 'resolved' as const,
      payload: toPlainPayload(input?.payload ?? { approved: false }),
    }
  })
}
