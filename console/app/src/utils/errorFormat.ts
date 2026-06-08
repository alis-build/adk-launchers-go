/**
 * Error formatting and logging for Connect-gRPC and AG-UI transports.
 *
 * Provides helpers to extract human-readable messages from `ConnectError`
 * instances, log structured error details, and detect specific gRPC error
 * codes (e.g. NOT_FOUND for missing threads).
 *
 * @module utils/errorFormat
 */
import { Code, ConnectError } from '@connectrpc/connect'

/**
 * Extracts a human-readable error message from an unknown error value.
 * Prefers the `rawMessage` from `ConnectError` (which omits the gRPC
 * status prefix), falls back to `Error.message`, and finally the
 * provided fallback string.
 *
 * @param err - The caught error (may be `ConnectError`, `Error`, or anything).
 * @param fallback - Default message returned when no useful message is found.
 * @returns A user-displayable error string.
 */
export function formatError(err: unknown, fallback: string): string {
  if (err instanceof ConnectError) {
    return err.rawMessage || err.message || fallback
  }
  if (err instanceof Error && err.message) return err.message
  return fallback
}

/**
 * Logs an error to the console with a scoped prefix.
 * For `ConnectError`, logs a structured object with message, code, and details.
 * For other errors, logs the raw value.
 *
 * @param scope - A label identifying the calling module (e.g. `'ThreadView'`).
 * @param err - The caught error value.
 */
export function logError(scope: string, err: unknown): void {
  if (err instanceof ConnectError) {
    console.error(`[${scope}] ConnectError`, {
      message: err.message,
      rawMessage: err.rawMessage,
      code: err.code,
      details: err.details,
    })
    return
  }
  console.error(`[${scope}]`, err)
}

/**
 * Checks whether an error represents a missing or inaccessible thread.
 * Returns `true` for gRPC `NOT_FOUND` and `PERMISSION_DENIED` codes,
 * which the backend returns for non-existent or unauthorized threads.
 *
 * @param err - The caught error value.
 * @returns `true` if the thread was not found or access was denied.
 */
export function isThreadNotFoundError(err: unknown): boolean {
  if (err instanceof ConnectError) {
    return err.code === Code.NotFound || err.code === Code.PermissionDenied
  }
  return false
}
