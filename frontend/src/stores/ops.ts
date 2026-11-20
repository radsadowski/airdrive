import { create } from 'zustand'
import type { AppEventMap } from '@/lib/api/contract'

export type OpProgress = AppEventMap['fileop:progress']

const LINGER_MS = 2600

interface OpsState {
  ops: Record<string, OpProgress>
  apply: (p: OpProgress) => void
}

/**
 * File operation progress (trash and in-place moves). The activity bar reads
 * this so non-transfer operations report progress the same way transfers and
 * syncs do; finished ops linger briefly so the last tick is visible.
 */
export const useOpsStore = create<OpsState>((set, get) => ({
  ops: {},

  apply(p) {
    set((s) => ({ ops: { ...s.ops, [p.opId]: p } }))
    if (p.state === 'running') return
    setTimeout(() => {
      const current = get().ops[p.opId]
      if (!current || current.state !== p.state) return
      const next = { ...get().ops }
      delete next[p.opId]
      set({ ops: next })
    }, LINGER_MS)
  },
}))
