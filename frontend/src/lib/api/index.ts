/**
 * Client factory — the mock|wails switch.
 *
 * Inside a Wails webview runs `wailsClient`; a plain browser (e.g. `vite dev`)
 * runs the stateful mock. Detection must be synchronous: the client is picked
 * at module scope, before Wails v3 injects window.wails (which happens on
 * DidFinishNavigation, i.e. after module scripts may already have run).
 */

import type { ApiClient, EventBus } from './contract'
import { mockClient } from './mock/client'
import { wailsClient } from './wails/client'

export type ClientMode = 'mock' | 'wails'

// insideWailsWebview detects the host synchronously: the macOS webview
// registers this message handler natively at DocumentStart, before any page
// script runs. window.wails arrives only at DidFinishNavigation, so checking
// for it here would race and intermittently pick the mock on cold starts.
function insideWailsWebview(): boolean {
  return (
    typeof window !== 'undefined' &&
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    !!(window as any).webkit?.messageHandlers?.external
  )
}

export function clientMode(): ClientMode {
  if (typeof window === 'undefined') return 'mock'
  // ?api=mock forces mock even inside the Wails window (useful for UI testing)
  if (new URLSearchParams(window.location.search).get('api') === 'mock') return 'mock'
  if (insideWailsWebview()) return 'wails'
  return 'mock'
}

const mode = clientMode()

export const api: ApiClient & EventBus = mode === 'wails' ? wailsClient : mockClient
export const isMock = mode === 'mock'

export type { ApiClient, EventBus }
