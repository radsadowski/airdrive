/**
 * File operations: every mutation funnels through here so refresh + toast
 * behavior is consistent across context menu, keyboard, and drag-drop.
 */

import { api } from '@/lib/api'
import { isInProgressError, toApiError } from '@/lib/api/contract-helpers'
import type { FileInfo } from '@/lib/api/contract'
import { openPrompt } from '@/components/prompt'
import { useAppStore } from '@/stores/app'
import { useUiStore, toastError } from '@/stores/ui'

function refresh(): void {
  void useAppStore.getState().refresh()
  void useAppStore.getState().reloadLocations()
}

export function newFolder(): void {
  const s = useAppStore.getState()
  if (!s.currentLocationId) return
  openPrompt({
    title: 'New folder',
    label: 'Folder name',
    initialValue: 'untitled folder',
    confirmLabel: 'Create',
    onConfirm: async (name) => {
      await api.mkdir(s.currentLocationId!, s.dirId, name)
      refresh()
    },
  })
}

export function renameSelection(file: FileInfo): void {
  const s = useAppStore.getState()
  if (!s.currentLocationId) return
  openPrompt({
    title: `Rename “${file.name}”`,
    label: 'New name',
    initialValue: file.name,
    confirmLabel: 'Rename',
    onConfirm: async (name) => {
      await api.rename(s.currentLocationId!, file.id, name)
      refresh()
    },
  })
}

let trashInFlight: Promise<void> | null = null
const inProgressNotified = new Map<string, number>()

function notifyInProgress(kind: 'delete' | 'move'): void {
  const now = Date.now()
  if (now - (inProgressNotified.get(kind) ?? 0) < 2000) return
  inProgressNotified.set(kind, now)
  useUiStore.getState().pushToast(
    kind === 'delete'
      ? {
          title: 'Delete already in progress',
          description: 'The selected items are already being moved to the trash.',
        }
      : {
          title: 'Move already in progress',
          description: 'The selected items are already being moved.',
        },
  )
}

export function trashSelection(): Promise<void> {
  if (trashInFlight) {
    notifyInProgress('delete')
    return trashInFlight
  }
  const s = useAppStore.getState()
  if (!s.currentLocationId || s.selection.length === 0) return Promise.resolve()
  const ids = [...s.selection]
  trashInFlight = api
    .deleteFiles(s.currentLocationId, ids)
    .then(() => {
      useAppStore.getState().clearSelection()
      refresh()
    })
    .catch((e) => {
      if (isInProgressError(e)) {
        notifyInProgress('delete')
        return
      }
      toastError(e)
    })
    .finally(() => {
      trashInFlight = null
    })
  return trashInFlight
}

export function downloadSelection(explicitIds?: string[]): void {
  const s = useAppStore.getState()
  const ids = explicitIds ?? s.selection
  if (!s.currentLocationId || ids.length === 0) return
  void api.download(s.currentLocationId, [...ids]).catch(toastError)
}

export function copyOrCutSelection(mode: 'copy' | 'cut'): void {
  const s = useAppStore.getState()
  if (!s.currentLocationId || s.selection.length === 0) return
  useUiStore.getState().setClipboard({
    mode,
    fileIds: [...s.selection],
    sourceLocationId: s.currentLocationId,
  })
  useUiStore.getState().pushToast({
    title: mode === 'copy' ? 'Copied' : 'Cut',
    description: `${s.selection.length} item${s.selection.length > 1 ? 's' : ''}: paste with ⌘V`,
  })
}

export async function pasteIntoCurrentDir(): Promise<void> {
  const s = useAppStore.getState()
  const clip = useUiStore.getState().clipboard
  if (!clip || !s.currentLocationId) return
  const { sourceLocationId, fileIds, mode } = clip

  try {
    if (sourceLocationId === s.currentLocationId) {
      if (mode === 'cut') {
        await api.moveWithin(sourceLocationId, fileIds, s.dirId)
        useUiStore.getState().setClipboard(null)
        refresh()
      } else {
        await api.enqueueTransfer({
          action: 'copy',
          fileIds,
          destLocationId: sourceLocationId,
          destDirId: s.dirId,
          conflictPolicy: 'smart',
        })
      }
    } else {
      await api.enqueueTransfer({
        action: mode === 'cut' ? 'move' : 'copy',
        fileIds,
        destLocationId: s.currentLocationId,
        destDirId: s.dirId,
        conflictPolicy: 'smart',
      })
      if (mode === 'cut') useUiStore.getState().setClipboard(null)
    }
  } catch (e) {
    if (isInProgressError(e)) {
      notifyInProgress('move')
      return
    }
    toastError(e)
  }
}

export function openTransferPicker(action: 'copy' | 'move'): void {
  const s = useAppStore.getState()
  if (!s.currentLocationId || s.selection.length === 0) return
  useUiStore.getState().setTransferPicker({ action, fileIds: [...s.selection] })
}

/** drop files from the host OS into the current dir */
export function uploadDroppedFiles(files: FileList | File[]): void {
  const s = useAppStore.getState()
  if (!s.currentLocationId) return
  const specs = Array.from(files).map((f) => ({ name: f.name, size: f.size }))
  if (specs.length === 0) return
  void api.uploadFiles(s.currentLocationId, s.dirId, specs).catch(toastError)
}

/** drag files onto a sidebar location → move there */
export function dropSelectionOnLocation(locationId: string, fileIds: string[], sourceLocationId: string): void {
  if (sourceLocationId === locationId) return
  void api
    .enqueueTransfer({
      action: 'move',
      fileIds,
      destLocationId: locationId,
      destDirId: null,
      conflictPolicy: 'smart',
    })
    .catch((e) => toastError(e))
}

export function err(e: unknown): void {
  const { code, message } = toApiError(e)
  useUiStore.getState().pushToast({ title: message, description: code !== 'unknown' ? `error: ${code}` : undefined, variant: 'error' })
}
