/**
 * Event wiring — the Go→UI contract events, wired into the stores.
 * Called once from main.tsx. Under Wails the same names arrive via Events.On;
 * under the mock they come from the in-memory bus.
 */

import { api } from '@/lib/api'
import { useAppStore } from '@/stores/app'
import { useTransfersStore } from '@/stores/transfers'
import { useUiStore } from '@/stores/ui'

export function wireEvents(): void {
  api.on('index:progress', ({ locationId, filesDone, filesTotal }) => {
    useAppStore.getState().setCrawlProgress(locationId, filesDone, filesTotal)
  })

  api.on('location:status', ({ locationId, state }) => {
    if (state === 'idle' || state === 'error') {
      useAppStore.getState().clearCrawlProgress(locationId)
      void useAppStore.getState().reloadLocations()
    }
  })

  api.on('fs:changed', ({ locationId, dirId }) => {
    void useAppStore.getState().applyFsChange(locationId, dirId)
  })

  api.on('transfer:updated', (u) => {
    useTransfersStore.getState().applyJobUpdate(u)
  })

  api.on('transfer:batchDone', (u) => {
    useTransfersStore.getState().applyBatchDone(u)
    if (u.failed > 0) {
      useUiStore.getState().pushToast({
        title: 'Transfer finished with problems',
        description: `${u.done} done · ${u.skipped} skipped · ${u.failed} failed`,
        variant: 'error',
      })
    } else if (u.done > 0 || u.skipped > 0) {
      useUiStore.getState().pushToast({
        title: 'Transfer complete',
        description: `${u.done} done · ${u.skipped} skipped`,
        variant: 'success',
      })
    }
  })

  api.on('location:status', () => {
    void useAppStore.getState().reloadLocations()
  })

  api.on('toast:show', (t) => {
    useUiStore.getState().pushToast(t)
  })
}
