import { create } from 'zustand'
import type { FileInfo, ListDirOptions, Location, SortSpec } from '@/lib/api/contract'
import { api } from '@/lib/api'
import { toApiError } from '@/lib/api/contract-helpers'

export interface Crumb {
  id: string | null
  name: string
}

export interface NavEntry {
  locationId: string
  dirId: string | null
  breadcrumbs: Crumb[]
}

const MAX_HISTORY = 100

interface AppState {
  locations: Location[]
  currentLocationId: string | null
  dirId: string | null
  breadcrumbs: Crumb[]
  items: FileInfo[]
  cursor: string | null
  loading: boolean
  error: string | null
  selection: string[]
  anchorId: string | null
  sort: SortSpec
  view: 'list' | 'grid'
  crawlProgress: Record<string, { done: number; total: number }>

  /** Browser-style global navigation history across locations+dirs. */
  history: NavEntry[]
  historyIndex: number   // points to the current entry in history

  init: () => Promise<void>
  selectLocation: (id: string | null, pushHistory?: boolean) => void
  openDir: (dir: { id: string | null; name: string }, pushHistory?: boolean) => Promise<void>
  navigateTo: (crumbIndex: number) => Promise<void>
  goUp: () => Promise<void>
  goBack: () => Promise<void>
  goForward: () => Promise<void>
  canGoBack: () => boolean
  canGoForward: () => boolean
  /** @internal — restores a history entry without pushing */ 
  _restoreEntry: (entry: NavEntry) => Promise<void>
  refresh: () => Promise<void>
  loadMore: () => Promise<void>
  setSort: (sort: SortSpec) => void
  setView: (view: 'list' | 'grid') => void
  applyFsChange: (locationId: string, dirId: string | null) => Promise<void>
  setSelection: (ids: string[], anchorId?: string | null) => void
  toggleSelect: (id: string, opts?: { additive?: boolean; range?: boolean }) => void
  selectAll: () => void
  clearSelection: () => void
  setCrawlProgress: (locationId: string, done: number, total: number) => void
  clearCrawlProgress: (locationId: string) => void
  reloadLocations: () => Promise<void>
  revealFile: (file: FileInfo) => Promise<void>
}

/** Push a new entry onto the history stack, dropping any forward history. */
function pushHistory(state: Pick<AppState, 'history' | 'historyIndex'>, entry: NavEntry): Pick<AppState, 'history' | 'historyIndex'> {
  const trimmed = state.history.slice(0, state.historyIndex + 1)
  const next = [...trimmed, entry].slice(-MAX_HISTORY)
  return { history: next, historyIndex: next.length - 1 }
}

export const useAppStore = create<AppState>((set, get) => ({
  locations: [],
  currentLocationId: null,
  dirId: null,
  breadcrumbs: [],
  items: [],
  cursor: null,
  loading: false,
  error: null,
  selection: [],
  anchorId: null,
  sort: { field: 'name', dir: 'asc' },
  view: 'list',
  crawlProgress: {},
  history: [],
  historyIndex: -1,

  setCrawlProgress(locationId: string, done: number, total: number) {
    set((s) => ({
      crawlProgress: { ...s.crawlProgress, [locationId]: { done, total } },
    }))
    if (total > 0 && done >= total) {
      setTimeout(() => {
        set((s) => {
          const next = { ...s.crawlProgress }
          delete next[locationId]
          return { crawlProgress: next }
        })
        void get().reloadLocations()
      }, 400)
    }
  },

  clearCrawlProgress(locationId: string) {
    set((s) => {
      const next = { ...s.crawlProgress }
      delete next[locationId]
      return { crawlProgress: next }
    })
  },

  async init() {
    await get().reloadLocations()
    const { locations } = get()
    if (locations.length > 0) {
      await get().selectLocation(locations[0].id)
    } else {
      set({ currentLocationId: null, dirId: null, breadcrumbs: [], items: [], cursor: null })
    }
  },

  async reloadLocations() {
    const locations = await api.listLocations()
    set({ locations })
  },

  async revealFile(file) {
    await get().selectLocation(file.locationId)
    const chain: { id: string; name: string }[] = []
    let cur = file.parentId
    let guard = 0
    while (cur && guard++ < 32) {
      const node = await api.stat(file.locationId, cur)
      chain.unshift({ id: node.id, name: node.name })
      cur = node.parentId
    }
    for (const c of chain) {
      await get().openDir({ id: c.id, name: c.name })
    }
    set({ selection: [file.id], anchorId: file.id })
  },

  async selectLocation(id, pushHistory_ = true) {
    if (!id) {
      set({ currentLocationId: null, dirId: null, breadcrumbs: [], items: [], cursor: null, selection: [] })
      return
    }
    if (!get().locations.some((l) => l.id === id)) {
      // Freshly created location (e.g. right after a wizard finishes) —
      // the sidebar's `locations` list won't show it until we refetch.
      await get().reloadLocations()
    }
    const loc = get().locations.find((l) => l.id === id)
    set({ currentLocationId: id, dirId: null, selection: [], error: null })
    const { root } = loc ?? { root: '/' }
    await get().openDir({ id: null, name: root }, pushHistory_)
  },

  async openDir(dir, pushHistory_ = true) {
    const { currentLocationId, sort } = get()
    if (!currentLocationId) return
    set({ loading: true, error: null, dirId: dir.id, cursor: null })
    try {
      const page = await api.listDir(currentLocationId, dir.id, { sort })
      set((s) => {
        const breadcrumbs =
          dir.id === null
            ? [{ id: null, name: dir.name }]
            : [...s.breadcrumbs, { id: dir.id, name: dir.name }]
        const entry: NavEntry = { locationId: currentLocationId, dirId: dir.id, breadcrumbs }
        const histUpdate = pushHistory_ ? pushHistory(s, entry) : {}
        return { items: page.items, cursor: page.cursor, breadcrumbs, loading: false, selection: [], ...histUpdate }
      })
    } catch (e) {
      set({ loading: false, error: toApiError(e).message, items: [] })
    }
  },

  async navigateTo(crumbIndex) {
    const { breadcrumbs } = get()
    const target = breadcrumbs[crumbIndex]
    if (!target) return
    if (crumbIndex === 0) {
      const loc = get().locations.find((l) => l.id === get().currentLocationId)
      await get().openDir({ id: null, name: loc?.root ?? '/' })
    } else {
      await get().openDir({ id: target.id!, name: target.name })
    }
    set((s) => ({ breadcrumbs: s.breadcrumbs.slice(0, crumbIndex + 1) }))
  },

  async goUp() {
    const { breadcrumbs } = get()
    if (breadcrumbs.length <= 1) return
    await get().navigateTo(breadcrumbs.length - 2)
  },

  canGoBack() {
    return get().historyIndex > 0
  },

  canGoForward() {
    const s = get()
    return s.historyIndex < s.history.length - 1
  },

  async goBack() {
    const { history, historyIndex } = get()
    if (historyIndex <= 0) return
    const entry = history[historyIndex - 1]
    set({ historyIndex: historyIndex - 1 })
    await get()._restoreEntry(entry)
  },

  async goForward() {
    const { history, historyIndex } = get()
    if (historyIndex >= history.length - 1) return
    const entry = history[historyIndex + 1]
    set({ historyIndex: historyIndex + 1 })
    await get()._restoreEntry(entry)
  },

  // Internal: restore a history entry without pushing to history.
  async _restoreEntry(entry: NavEntry) {
    const { sort } = get()
    set({
      currentLocationId: entry.locationId,
      dirId: entry.dirId,
      breadcrumbs: entry.breadcrumbs,
      loading: true,
      error: null,
      selection: [],
      cursor: null,
    })
    try {
      const page = await api.listDir(entry.locationId, entry.dirId, { sort })
      set({ items: page.items, cursor: page.cursor, loading: false })
    } catch (e) {
      set({ loading: false, error: toApiError(e).message, items: [] })
    }
  },

  async refresh() {
    const { currentLocationId, dirId, sort } = get()
    if (!currentLocationId) return
    set({ loading: true, error: null })
    try {
      const page = await api.listDir(currentLocationId, dirId, { sort })
      set({ items: page.items, cursor: page.cursor, loading: false })
    } catch (e) {
      set({ loading: false, error: toApiError(e).message })
    }
  },

  async loadMore() {
    const { currentLocationId, dirId, cursor, items, sort, loading } = get()
    if (!currentLocationId || !cursor || loading) return
    set({ loading: true })
    try {
      const page = await api.listDir(currentLocationId, dirId, { sort, cursor, limit: 400 } as ListDirOptions)
      set({ items: [...items, ...page.items], cursor: page.cursor, loading: false })
    } catch {
      set({ loading: false })
    }
  },

  setSort(sort) {
    set({ sort })
    void get().refresh()
  },

  setView(view) {
    set({ view })
  },

  async applyFsChange(locationId, dirId) {
    const { currentLocationId, dirId: openDirId, loading } = get()
    if (locationId !== currentLocationId) {
      void get().reloadLocations()
      return
    }
    if (dirId === null || dirId === openDirId || loading) {
      void get().refresh()
      void get().reloadLocations()
    }
  },

  setSelection(ids, anchorId = null) {
    set({ selection: ids, anchorId })
  },

  toggleSelect(id, opts) {
    const { selection, items, anchorId } = get()
    if (opts?.range && anchorId) {
      const a = items.findIndex((n) => n.id === anchorId)
      const b = items.findIndex((n) => n.id === id)
      if (a >= 0 && b >= 0) {
        const [from, to] = a < b ? [a, b] : [b, a]
        const rangeIds = items.slice(from, to + 1).map((n) => n.id)
        const sel = new Set([...selection, ...rangeIds])
        set({ selection: [...sel] })
        return
      }
    }
    if (opts?.additive) {
      const sel = new Set(selection)
      if (sel.has(id)) sel.delete(id)
      else sel.add(id)
      set({ selection: [...sel], anchorId: id })
    } else {
      set({ selection: [id], anchorId: id })
    }
  },

  selectAll() {
    set({ selection: get().items.map((n) => n.id) })
  },

  clearSelection() {
    set({ selection: [], anchorId: null })
  },
}))
