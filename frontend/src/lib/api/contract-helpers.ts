/** Shared client-side helpers for both ApiClient implementations. */

export class ApiError extends Error {
  code: string
  retryable: boolean
  constructor(code: string, message: string, retryable = false) {
    super(message)
    this.code = code
    this.retryable = retryable
  }
}

/** normalize any thrown value into an ApiError-ish for the UI */
export function toApiError(e: unknown): { code: string; message: string; retryable: boolean } {
  if (e && typeof e === 'object' && 'code' in e && 'message' in e) {
    const err = e as { code: string; message: string; retryable?: boolean }
    return { code: String(err.code), message: String(err.message), retryable: Boolean(err.retryable) }
  }
  if (e instanceof Error) return { code: 'unknown', message: e.message, retryable: false }
  return { code: 'unknown', message: String(e), retryable: false }
}
