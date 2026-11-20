import { create } from 'zustand'
import type { FileInfo, Settings, ToastPayload } from '@/lib/api/contract'
import { api } from '@/lib/api'

export type WizardKind = 'picker' | 'local' | 'drive' | 's3'

interface UiState {
  toasts: ToastPayload[]
  sidebarOpen: boolean
  searchOpen: boolean
  inspectorOpen: boolean
  inspectedFile: FileInfo | null
  wizard: WizardKind | null
  settingsOpen: boolean
  settings: Settings
  syncOpen: boolean
  /** Move/Copy-to picker payload */
  transferPicker: { action: 'copy' | 'move'; fileIds: string[] } | null
  /** in-app clipboard for ⌘C/⌘X/⌘V */
  clipboard: { mode: 'copy' | 'cut'; fileIds: string[]; sourceLocationId: string } | null
  /** auto-opened prompt for a location whose credentials must be renewed */
  reconnectPrompt: { locationId: string; name: string; error: string } | null
  /** confirmation prompt before cancelling a running file operation */
  cancelPrompt: { opId: string; label: string } | null
  logsOpen: boolean

  pushToast: (t: Omit<ToastPayload, 'id'>) => void
  dismissToast: (id: string) => void
  setSidebarOpen: (open: boolean) => void
  toggleSidebar: () => void
  setSearchOpen: (open: boolean) => void
  setInspectorOpen: (open: boolean) => void
  inspect: (file: FileInfo | null) => void
  openWizard: (kind: WizardKind | null) => void
  setSettingsOpen: (open: boolean) => void
  setSyncOpen: (open: boolean) => void
  loadSettings: () => Promise<void>
  patchSettings: (patch: Partial<Settings>) => Promise<void>
  setTransferPicker: (p: { action: 'copy' | 'move'; fileIds: string[] } | null) => void
  setClipboard: (c: UiState['clipboard']) => void
  promptReconnect: (p: { locationId: string; name: string; error: string }) => void
  dismissReconnect: () => void
  resolveReconnect: (locationId: string) => void
  askCancelOp: (p: { opId: string; label: string }) => void
  dismissCancelOp: () => void
  setLogsOpen: (open: boolean) => void
}

let toastSeq = 0
const reconnectPrompted = new Set<string>()

export const useUiStore = create<UiState>((set, get) => ({
  toasts: [],
  sidebarOpen: true,
  searchOpen: false,
  inspectorOpen: true,
  inspectedFile: null,
  wizard: null,
  settingsOpen: false,
  syncOpen: false,
  settings: {
    theme: 'dark',
    defaultView: 'list',
    concurrentTransfers: 3,
    showHiddenFiles: false,
    downloadDir: '/Users/you/Downloads',
    debugLog: false,
  },
  transferPicker: null,
  clipboard: null,
  reconnectPrompt: null,
  cancelPrompt: null,
  logsOpen: false,

  pushToast(t) {
    const id = `toast_${Date.now()}_${toastSeq++}`
    set((s) => ({ toasts: [...s.toasts.slice(-4), { ...t, id }] }))
    setTimeout(() => get().dismissToast(id), t.variant === 'error' ? 7000 : 5200)
  },

  dismissToast(id) {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
  },

  setSidebarOpen(open) {
    set({ sidebarOpen: open })
  },

  toggleSidebar() {
    set((s) => ({ sidebarOpen: !s.sidebarOpen }))
  },

  setSearchOpen(open) {
    set({ searchOpen: open })
  },

  setInspectorOpen(open) {
    set({ inspectorOpen: open })
  },

  inspect(file) {
    set({ inspectedFile: file, inspectorOpen: file !== null })
  },

  openWizard(kind) {
    set({ wizard: kind })
  },

  setSettingsOpen(open) {
    set({ settingsOpen: open })
  },

  setSyncOpen(open) {
    set({ syncOpen: open })
  },

  async loadSettings() {
    const settings = await api.getSettings()
    set({ settings })
  },

  async patchSettings(patch) {
    const settings = await api.updateSettings(patch)
    set({ settings })
  },

  setTransferPicker(p) {
    set({ transferPicker: p })
  },

  setClipboard(c) {
    set({ clipboard: c })
  },

  promptReconnect(p) {
    if (reconnectPrompted.has(p.locationId)) return
    reconnectPrompted.add(p.locationId)
    set({ reconnectPrompt: p })
  },

  dismissReconnect() {
    set({ reconnectPrompt: null })
  },

  resolveReconnect(locationId) {
    reconnectPrompted.delete(locationId)
    set({ reconnectPrompt: null })
  },

  askCancelOp(p) {
    set({ cancelPrompt: p })
  },

  dismissCancelOp() {
    set({ cancelPrompt: null })
  },

  setLogsOpen(open) {
    set({ logsOpen: open })
  },
}))

/** convenience: toast an error */
export function toastError(e: unknown) {
  const msg = e instanceof Error ? e.message : String(e)
  useUiStore.getState().pushToast({ title: 'Something went wrong', description: msg, variant: 'error' })
}

export function toastSuccess(title: string, description?: string) {
  useUiStore.getState().pushToast({ title, description, variant: 'success' })
}
