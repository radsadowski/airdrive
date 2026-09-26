import { useEffect, useState } from 'react'

/**
 * Shell-level Go→UI loop proof (not part of the ApiClient contract).
 *
 * Calls the real Wails binding (internal/bridge AppService.Version) and races
 * it against a timeout: inside the Wails webview it resolves with the backend
 * version; in a plain browser it falls back to the mock label. When Phase 2+
 * services land, this is the pattern the WailsClient uses for everything else.
 */

const TIMEOUT_MS = 1500

export type BackendPresence = 'wails' | 'browser'

export function useBackendVersion(): { version: string; presence: BackendPresence | 'unknown' } {
  const [state, setState] = useState<{ version: string; presence: BackendPresence | 'unknown' }>({
    version: '…',
    presence: 'unknown',
  })

  useEffect(() => {
    let alive = true
    const timeout = new Promise<never>((_, reject) => setTimeout(() => reject(new Error('timeout')), TIMEOUT_MS))
    import('@bindings/github.com/radosny/airdrive/internal/bridge/appservice')
      .then((bindings) => Promise.race([bindings.Version(), timeout]))
      .then((version) => {
        if (alive) setState({ version: `v${version} · wails`, presence: 'wails' })
      })
      .catch(() => {
        if (alive) setState({ version: 'v0.1 · browser mock', presence: 'browser' })
      })
    return () => {
      alive = false
    }
  }, [])

  return state
}
