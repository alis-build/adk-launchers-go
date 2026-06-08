/**
 * Detection and user-friendly messaging for HITL resume-required run errors.
 *
 * @module pages/threads/hitlRunErrors
 */

/** Returns true when the server error indicates a pending interrupt must be resolved first. */
export function isResumeRequiredRunError(message: string): boolean {
  const lower = message.toLowerCase()
  return lower.includes('resume is required') || lower.includes('pending interrupt')
}

/** User-facing copy when a run is blocked by open tool confirmation. */
export function friendlyResumeRequiredMessage(alreadyShowingInterruptUi: boolean): string {
  if (alreadyShowingInterruptUi) {
    return 'Resolve the tool confirmation above before continuing.'
  }
  return 'This thread is waiting for tool confirmation. Reload the page if you do not see the permission card.'
}
