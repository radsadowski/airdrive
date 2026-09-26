/**
 * MockState — the in-memory filesystem backing the MockClient.
 * Every operation mutates this tree; rename renames, move moves, delete trashes.
 */

import type {
  FileInfo,
  ListDirOptions,
  Location,
  Page,
  SearchQuery,
  Settings,
  SortSpec,
  TransferBatch,
  TransferJob,
} from '../contract'
import { fileMeta, genSubtree, makeFile, makeDir, Rng, type LocationPreset } from './data'
import { ApiError } from '../contract-helpers'

export { ApiError }

export interface MockFileNode extends FileInfo {
  trashed: boolean
}

const DEFAULT_SETTINGS: Settings = {
  theme: 'dark',
  defaultView: 'list',
  concurrentTransfers: 3,
  bandwidthLimitMbps: 80,
  showHiddenFiles: false,
  downloadDir: '/Users/you/Downloads',
}

export function sortComparator(spec: SortSpec): (a: FileInfo, b: FileInfo) => number {
  const dir = spec.dir === 'asc' ? 1 : -1
  const natCmp = new Intl.Collator('en', { numeric: true, sensitivity: 'base' })
  return (a, b) => {
    if (a.kind !== b.kind) return a.kind === 'dir' ? -1 : 1
    switch (spec.field) {
      case 'name':
        return natCmp.compare(a.name, b.name) * dir
      case 'size':
        return (a.size - b.size) * dir
      case 'modifiedAt':
        return (a.modifiedAt < b.modifiedAt ? -1 : 1) * dir
      case 'kind':
        return natCmp.compare(a.mime, b.mime) * dir || natCmp.compare(a.name, b.name)
      default:
        return 0
    }
  }
}

export class MockState {
  locations = new Map<string, Location>()
  files = new Map<string, MockFileNode>()
  /** parentId → child ids; roots use `root:<locationId>` */
  children = new Map<string, string[]>()
  batches: TransferBatch[] = []
  jobs: TransferJob[] = []
  settings: Settings = { ...DEFAULT_SETTINGS }

  rootKey(locationId: string): string {
    return `root:${locationId}`
  }

  addChild(locationId: string, parentId: string | null, node: MockFileNode): void {
    node.locationId = locationId
    node.parentId = parentId
    this.files.set(node.id, node)
    const key = parentId ?? this.rootKey(locationId)
    const arr = this.children.get(key)
    if (arr) arr.push(node.id)
    else this.children.set(key, [node.id])
  }

  removeChild(node: MockFileNode): void {
    this.files.delete(node.id)
    const key = node.parentId ?? this.rootKey(node.locationId)
    const arr = this.children.get(key)
    if (arr) {
      const i = arr.indexOf(node.id)
      if (i >= 0) arr.splice(i, 1)
    }
  }

  siblings(locationId: string, parentId: string | null): MockFileNode[] {
    const key = parentId ?? this.rootKey(locationId)
    return (this.children.get(key) ?? []).map((id) => this.files.get(id)!).filter((n) => n && !n.trashed)
  }

  node(fileId: string): MockFileNode {
    const n = this.files.get(fileId)
    if (!n) throw new ApiError('not_found', 'File not found')
    return n
  }

  /** visible, sorted children page */
  listPage(locationId: string, dirId: string | null, opts: ListDirOptions): Page<FileInfo> {
    const includeHidden = opts.includeHidden ?? this.settings.showHiddenFiles
    let items = this.siblings(locationId, dirId).map((n) => n as FileInfo)
    if (!includeHidden) items = items.filter((n) => !n.name.startsWith('.'))
    items.sort(sortComparator(opts.sort ?? { field: 'name', dir: 'asc' }))
    const limit = opts.limit ?? 400
    const offset = opts.cursor ? parseInt(opts.cursor, 10) : 0
    const page = items.slice(offset, offset + limit)
    return {
      items: page,
      cursor: offset + limit < items.length ? String(offset + limit) : null,
    }
  }

  pathOf(fileId: string): string {
    const parts: string[] = []
    let cur = this.files.get(fileId)
    while (cur) {
      parts.unshift(cur.name)
      cur = cur.parentId ? this.files.get(cur.parentId) : undefined
    }
    return '/' + parts.join('/')
  }

  /** mkdir -p equivalent; returns final dir id (null = location root when base null & no segments) */
  ensureDirPath(locationId: string, baseId: string | null, segments: string[]): string | null {
    let parent = baseId
    for (const seg of segments) {
      if (!seg) continue
      const existing = this.siblings(locationId, parent).find(
        (n) => n.kind === 'dir' && n.name.toLowerCase() === seg.toLowerCase(),
      )
      if (existing) {
        parent = existing.id
      } else {
        const dir = { ...makeDir(locationId, parent, seg, seg), trashed: false }
        this.addChild(locationId, parent, dir)
        parent = dir.id
      }
    }
    return parent
  }

  /** next free "name (N).ext" for keep-both conflicts */
  dedupeName(locationId: string, parentId: string | null, name: string): string {
    const taken = new Set(this.siblings(locationId, parentId).map((n) => n.name.toLowerCase()))
    if (!taken.has(name.toLowerCase())) return name
    const dot = name.lastIndexOf('.')
    const stem = dot > 0 ? name.slice(0, dot) : name
    const ext = dot > 0 ? name.slice(dot) : ''
    for (let i = 2; i < 1000; i++) {
      const candidate = `${stem} (${i})${ext}`
      if (!taken.has(candidate.toLowerCase())) return candidate
    }
    return `${stem} (${Date.now()})${ext}`
  }

  /** recursive expansion of selection into individual files + relative subpaths */
  expandSelection(fileIds: string[]): { fileId: string; relPath: string[] }[] {
    const out: { fileId: string; relPath: string[] }[] = []
    const walk = (id: string, rel: string[]) => {
      const n = this.files.get(id)
      if (!n || n.trashed) return
      if (n.kind === 'file') out.push({ fileId: id, relPath: rel })
      else {
        for (const childId of this.children.get(n.id) ?? []) walk(childId, [...rel, n.name])
      }
    }
    for (const id of fileIds) walk(id, [])
    return out
  }

  countsFor(locationId: string): { fileCount: number; totalSize: number } {
    let fileCount = 0
    let totalSize = 0
    for (const n of this.files.values()) {
      if (n.locationId === locationId && !n.trashed && n.kind === 'file') {
        fileCount++
        totalSize += n.size
      }
    }
    return { fileCount, totalSize }
  }

  search(q: SearchQuery): Page<FileInfo> {
    const terms = q.text.toLowerCase().split(/\s+/).filter(Boolean)
    const results: FileInfo[] = []
    for (const n of this.files.values()) {
      if (n.trashed) continue
      if (terms.length && !terms.every((t) => n.name.toLowerCase().includes(t))) continue
      if (q.locationIds?.length && !q.locationIds.includes(n.locationId)) continue
      if (q.categories?.length) {
        const { category } = fileMeta(n.name)
        if (!q.categories.includes(category)) continue
      }
      if (q.sizeMin !== undefined && n.size < q.sizeMin) continue
      if (q.sizeMax !== undefined && n.size > q.sizeMax) continue
      if (q.modifiedFrom && n.modifiedAt < q.modifiedFrom) continue
      if (q.modifiedTo && n.modifiedAt > q.modifiedTo) continue
      results.push({ ...n })
    }
    results.sort((a, b) => (a.modifiedAt < b.modifiedAt ? 1 : -1))
    const limit = q.limit ?? 150
    const offset = q.cursor ? parseInt(q.cursor, 10) : 0
    const page = results.slice(offset, offset + limit)
    return { items: page, cursor: offset + limit < results.length ? String(offset + limit) : null }
  }

  loadPreset(preset: LocationPreset, seed: number): Location {
    const locationId = `loc_${preset.kind}_${seed}`
    const rng = new Rng(seed * 7 + 3)
    for (const spec of preset.specs) {
      const out: FileInfo[] = []
      genSubtree(locationId, null, spec, out, rng, `${preset.root}/${spec.name}`)
      for (const node of out) {
        this.addChild(locationId, node.parentId, { ...node, trashed: false })
      }
    }
    this.sprinkleDotFiles(locationId, seed)
    const { fileCount, totalSize } = this.countsFor(locationId)
    const loc: Location = {
      id: locationId,
      kind: preset.kind,
      name: preset.name,
      root: preset.root,
      status: 'idle',
      fileCount,
      totalSize,
      lastSyncedAt: new Date(Date.now() - rng.int(1, 9) * 60_000).toISOString(),
    }
    this.locations.set(locationId, loc)
    return loc
  }

  /** sprinkle macOS noise so the hidden-files toggle does something */
  sprinkleDotFiles(locationId: string, seed: number): void {
    const rng = new Rng(seed)
    for (const [key, ids] of this.children) {
      if (key.startsWith('root:')) {
        if (key !== this.rootKey(locationId)) continue
      } else {
        const dir = this.files.get(key)
        if (!dir || dir.locationId !== locationId) continue
      }
      if (ids.length > 0 && rng.next() < 0.3) {
        const parentId = key.startsWith('root:') ? null : key
        const f = { ...makeFile(locationId, parentId, '.DS_Store', rng, 'noise'), trashed: false }
        this.files.set(f.id, f)
        ids.push(f.id)
      }
    }
  }
}
