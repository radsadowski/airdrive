import { create } from 'zustand'
import type { SyncPair } from '@/lib/api/contract'
import { api } from '@/lib/api'

/**
 * Sync store: the pair rows plus the last-known phase per row. Coarse by
 * design: reconcile progress surfaces through the Transfers drawer, this
 * store just keeps the policies themselves fresh.
 */
interface SyncState {
  pairs: SyncPair[]
  loaded: boolean
  load: () => Promise<void>
  save: (pair: SyncPair, isNew: boolean) => Promise<SyncPair>
  remove: (id: string) => Promise<void>
  setPaused: (id: string, paused: boolean) => Promise<void>
  runNow: (id: string) => Promise<void>
}

export const useSyncStore = create<SyncState>((set, get) => ({
  pairs: [],
  loaded: false,

  async load() {
    const pairs = await api.listSyncPairs()
    set({ pairs, loaded: true })
  },

  async save(pair, isNew) {
    const saved = isNew ? await api.createSyncPair(pair) : await api.updateSyncPair(pair)
    await get().load()
    return saved
  },

  async remove(id) {
    await api.deleteSyncPair(id)
    set((s) => ({ pairs: s.pairs.filter((p) => p.id !== id) }))
  },

  async setPaused(id, paused) {
    await api.pauseSync(id, paused)
    set((s) => ({ pairs: s.pairs.map((p) => (p.id === id ? { ...p, paused } : p)) }))
  },

  async runNow(id) {
    await api.syncNow(id)
  },
}))

/** decode the backend's JSON counts column */
export function parseCounts(json?: string): { copied: number; deleted: number; skipped: number; failed: number } | null {
  if (!json) return null
  try {
    const parsed = JSON.parse(json)
    return {
      copied: Number(parsed.copied) || 0,
      deleted: Number(parsed.deleted) || 0,
      skipped: Number(parsed.skipped) || 0,
      failed: Number(parsed.failed) || 0,
    }
  } catch {
    return null
  }
}
