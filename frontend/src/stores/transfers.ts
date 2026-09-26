import { create } from 'zustand'
import type { TransferBatch, TransferJob, TransferState } from '@/lib/api/contract'
import { api } from '@/lib/api'

export interface BatchView extends TransferBatch {
  /** live aggregate derived from jobs */
  jobs: TransferJob[]
  bytesDone: number
  bytesTotal: number
  speed: number
  doneCount: number
  failedCount: number
  skippedCount: number
  cancelledCount: number
  pendingCount: number
  active: boolean
}

function buildBatchViews(batches: TransferBatch[], jobs: TransferJob[]): BatchView[] {
  const byBatch = new Map<string, TransferJob[]>()
  for (const j of jobs) {
    const arr = byBatch.get(j.batchId)
    if (arr) arr.push(j)
    else byBatch.set(j.batchId, [j])
  }
  return batches.map((b) => {
    const js = byBatch.get(b.id) ?? []
    return {
      ...b,
      jobs: js,
      bytesDone: js.reduce((a, j) => a + j.bytesDone, 0),
      bytesTotal: js.reduce((a, j) => a + j.bytesTotal, 0),
      speed: js.reduce((a, j) => a + (j.state === 'running' ? j.speed : 0), 0),
      doneCount: js.filter((j) => j.state === 'done').length,
      failedCount: js.filter((j) => j.state === 'failed').length,
      skippedCount: js.filter((j) => j.state === 'skipped').length,
      cancelledCount: js.filter((j) => j.state === 'cancelled').length,
      pendingCount: js.filter((j) => j.state === 'pending' || j.state === 'running' || j.state === 'paused').length,
      active: js.some((j) => j.state === 'running' || j.state === 'pending' || j.state === 'paused'),
    }
  })
}

interface TransfersState {
  batches: BatchView[]
  drawerOpen: boolean

  hydrate: () => Promise<void>
  setDrawerOpen: (open: boolean) => void
  applyJobUpdate: (u: {
    batchId: string
    jobId: string
    state: TransferState
    bytesDone: number
    bytesTotal: number
    speed: number
  }) => void
  applyBatchDone: (u: { batchId: string; done: number; skipped: number; failed: number }) => void
}

export const useTransfersStore = create<TransfersState>((set, get) => ({
  batches: [],
  drawerOpen: false,

  async hydrate() {
    const batches = await api.listBatches()
    const jobs = await api.listJobs(batches.map((b) => b.id))
    set({ batches: buildBatchViews(batches, jobs) })
  },

  setDrawerOpen(open) {
    set({ drawerOpen: open })
  },

  applyJobUpdate(u) {
    const existing = get().batches.find((b) => b.id === u.batchId)
    if (!existing) {
      void get().hydrate()
      return
    }
    const jobs = existing.jobs.map((j) =>
      j.id === u.jobId
        ? { ...j, state: u.state, bytesDone: u.bytesDone, bytesTotal: u.bytesTotal, speed: u.speed }
        : j,
    )
    const view = buildBatchViews([existing], jobs)[0]
    set((s) => ({ batches: s.batches.map((b) => (b.id === view.id ? view : b)) }))
  },

  applyBatchDone(_u) {
    void get().hydrate()
  },
}))
