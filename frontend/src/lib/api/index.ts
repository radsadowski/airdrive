/**
 * Client factory — the mock|wails switch.
 *
 * `?api=wails` selects the Wails bindings client; everything else (including
 * plain `vite dev` in a browser) runs the stateful mock. Phase 2+ flips
 * services to real implementations one vertical slice at a time.
 */

import type { ApiClient, EventBus } from './contract'
import { mockClient } from './mock/client'
import { wailsClient } from './wails/client'

export type ClientMode = 'mock' | 'wails'

export function clientMode(): ClientMode {
  if (typeof window === 'undefined') return 'mock'
  // ?api=mock forces mock even inside the Wails window (useful for UI testing)
  if (new URLSearchParams(window.location.search).get('api') === 'mock') return 'mock'
  // Wails v3 injects window.wails synchronously before any scripts run.
  if ('wails' in window) return 'wails'
  return 'mock'
}

const mode = clientMode()

export const api: ApiClient & EventBus = mode === 'wails' ? wailsClient : mockClient
export const isMock = mode === 'mock'

export type { ApiClient, EventBus }
