/**
 * MockClient — a stateful in-memory implementation of the ApiClient contract.
 * The whole product runs against this in a plain browser. Operations mutate
 * the tree; transfers simulate; crawls emit progress; auth is theater.
 */

import type {
  AddLocalRequest,
  ApiClient,
  EventBus,
  FileInfo,
  GdriveSession,
  Location,
  S3LocationConfig,
  Settings,
  TransferBatch,
  TransferJob,
  ToastPayload,
} from '../contract'
import { FAKE_ACCOUNTS, PRESETS, STRESS_PRESET, nextId, fileMeta, makeDir, makeFile, Rng } from './data'
import { bus } from './bus'
import { ApiError, MockState } from './state'
import { TransferEngine, runFakeCrawl } from './runtime'

/* ------------------------------------------------------------------ */
/* Dev controls (exported for the dev panel — mock only)               */
/* ------------------------------------------------------------------ */

export interface MockDevControls {
  failNextCall(): void
  addStressLocation(): void
  reset(): void
  clearAll(): void
  setOffline(v: boolean): void
  isOffline(): boolean
  setLatency(v: number): void
  getLatency(): number
  hasStress(): boolean
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

/** deterministic fake Drive folder tree for the connect-flow picker */
const DRIVE_FOLDERS: { id: string; parentId: string; name: string; starred?: boolean }[] = [
  { id: 'f_photos', parentId: 'root', name: 'Photos' },
  { id: 'f_photos_2024', parentId: 'f_photos', name: '2024' },
  { id: 'f_photos_2025', parentId: 'f_photos', name: '2025' },
  { id: 'f_docs', parentId: 'root', name: 'Documents' },
  { id: 'f_docs_invoices', parentId: 'f_docs', name: 'Invoices' },
  { id: 'f_backups', parentId: 'root', name: 'Backups' },
  { id: 'f_projects', parentId: 'root', name: 'Projects', starred: true },
  { id: 'f_computer_docs', parentId: 'device_1', name: 'MacBook · Documents', starred: true },
  { id: 'f_computer_desktop', parentId: 'device_1', name: 'MacBook · Desktop', starred: true },
]

class MockClient implements ApiClient {
  readonly state = new MockState()
  readonly engine = new TransferEngine({ state: this.state, bus })
  latencyMultiplier = 1
  failNext = false
  private gdriveSessions = new Map<string, (typeof FAKE_ACCOUNTS)[number]>()

  constructor() {
    this.seed()
    this.engine.start()
  }

  /* ---- boot ---- */

  seed(): void {
    // first run: a live cockpit — local Documents, Drive, one R2 bucket
    this.state.loadPreset(PRESETS[0], PRESETS[0].seed)
    this.state.loadPreset(PRESETS[2], PRESETS[2].seed)
    this.state.loadPreset(PRESETS[3], PRESETS[3].seed)
  }

  private async net(kind: Location['kind'], baseMs: number): Promise<void> {
    if (this.failNext) {
      this.failNext = false
      throw new ApiError('injected', 'Request failed (injected via dev panel)', true)
    }
    if (this.engine.offline && kind !== 'localfs') {
      throw new ApiError('offline', `${kind === 'gdrive' ? 'Google Drive' : 'S3'} is unreachable — you are offline`, true)
    }
    const jitter = 0.7 + Math.random() * 0.6
    await sleep(baseMs * jitter * this.latencyMultiplier)
  }

  private touch(locationId: string, dirId: string | null): void {
    bus.emit('fs:changed', { locationId, dirId })
  }

  private refreshCounts(locationId: string): void {
    const loc = this.state.locations.get(locationId)
    if (!loc) return
    const { fileCount, totalSize } = this.state.countsFor(locationId)
    loc.fileCount = fileCount
    loc.totalSize = totalSize
  }

  private toast(title: string, description?: string, variant?: ToastPayload['variant'], actionLabel?: string): void {
    bus.emit('toast:show', {
      id: nextId('t'),
      title,
      description,
      variant,
      actionLabel,
      actionToken: nextId('act'),
    })
  }

  /* ---- locations ---- */

  async listLocations(): Promise<Location[]> {
    await sleep(40)
    return [...this.state.locations.values()].map((l) => ({ ...l }))
  }

  async addLocalLocation(req: AddLocalRequest): Promise<Location> {
    await this.net('localfs', 180)
    const preset: (typeof PRESETS)[number] = {
      kind: 'localfs',
      name: req.name ?? `Local — ${req.root.split('/').pop() || req.root}`,
      root: req.root,
      seed: 3000 + Math.floor(Math.random() * 900),
      specs: [{ name: req.root.split('/').pop() || 'root', files: 40 + Math.floor(Math.random() * 120), subdirs: 3 }],
    }
    const loc = this.state.loadPreset(preset, preset.seed)
    runFakeCrawl(this.state, bus, loc.id, loc.fileCount)
    return { ...loc }
  }

  async connectGdrive(): Promise<GdriveSession> {
    // Real sign-in is a single round trip through the system browser with no
    // in-app account chooser — simulate that latency, then "pick" the first
    // fake account, mirroring FetchAccountInfo() on the real backend.
    await this.net('gdrive', 1400)
    const account = FAKE_ACCOUNTS[0]
    const sessionId = `gdsess_${nextId('s')}`
    this.gdriveSessions.set(sessionId, account)
    return { sessionId, email: account.email, name: account.name }
  }

  async browseGdriveFolders(sessionId: string, dirId: string | null): Promise<FileInfo[]> {
    await this.net('gdrive', 220)
    const account = this.gdriveSessions.get(sessionId)
    if (!account) throw new ApiError('expired', 'google sign-in session expired — start again', true)
    return DRIVE_FOLDERS.filter((f) => f.parentId === (dirId ?? 'root')).map((f) => ({
      id: f.id,
      locationId: '',
      parentId: f.parentId === 'root' ? null : f.parentId,
      name: f.name,
      kind: 'dir' as const,
      size: 0,
      mime: 'application/vnd.google-apps.folder',
      modifiedAt: new Date().toISOString(),
      etag: f.id,
    }))
  }

  async starredGdriveFolders(sessionId: string): Promise<FileInfo[]> {
    await this.net('gdrive', 220)
    const account = this.gdriveSessions.get(sessionId)
    if (!account) throw new ApiError('expired', 'google sign-in session expired — start again', true)
    return DRIVE_FOLDERS.filter((f) => f.starred).map((f) => ({
      id: f.id,
      locationId: '',
      parentId: f.parentId === 'root' ? null : f.parentId,
      name: f.name,
      kind: 'dir' as const,
      size: 0,
      mime: 'application/vnd.google-apps.folder',
      modifiedAt: new Date().toISOString(),
      etag: f.id,
    }))
  }

  async confirmGdriveLocation(sessionId: string, folderId: string | null): Promise<Location> {
    await this.net('gdrive', 600)
    const account = this.gdriveSessions.get(sessionId)
    if (!account) throw new ApiError('expired', 'google sign-in session expired — start again', true)
    this.gdriveSessions.delete(sessionId)
    const folder = folderId ? DRIVE_FOLDERS.find((f) => f.id === folderId) : null
    const preset = PRESETS[2]
    const loc = this.state.loadPreset(
      {
        ...preset,
        name: folder ? `${account.name} · ${folder.name}` : `Google Drive — ${account.email}`,
        root: folder?.name ?? 'My Drive',
      },
      4400 + Math.floor(Math.random() * 99),
    )
    runFakeCrawl(this.state, bus, loc.id, loc.fileCount)
    return { ...loc }
  }

  async addS3Location(cfg: S3LocationConfig): Promise<Location> {
    await this.net('s3', 800)
    const preset: (typeof PRESETS)[number] = {
      kind: 's3',
      name: `${cfg.preset.toUpperCase()} — ${cfg.bucket}`,
      root: cfg.bucket,
      seed: 5000 + Math.floor(Math.random() * 900),
      specs: [{ name: cfg.bucket, files: 200 + Math.floor(Math.random() * 600), subdirs: 4 }],
    }
    const loc = this.state.loadPreset(preset, preset.seed)
    runFakeCrawl(this.state, bus, loc.id, loc.fileCount)
    return { ...loc }
  }

  async testS3Connection(cfg: S3LocationConfig): Promise<{ ok: boolean; buckets: string[]; error?: string }> {
    await this.net('s3', 650)
    if (this.engine.offline) {
      return { ok: false, buckets: [], error: 'endpoint unreachable — check network' }
    }
    if (!cfg.endpoint.startsWith('http') || !cfg.accessKeyId) {
      return { ok: false, buckets: [], error: 'invalid endpoint or credentials' }
    }
    return {
      ok: true,
      buckets: [cfg.bucket || 'airdrive-prod', 'backups-archive', 'media-cache'].filter(
        (b, i, arr) => arr.indexOf(b) === i,
      ),
    }
  }

  async removeLocation(locationId: string): Promise<void> {
    const loc = this.state.locations.get(locationId)
    if (!loc) return
    await this.net(loc.kind, 200)
    this.state.locations.delete(locationId)
    for (const [id, n] of [...this.state.files]) {
      if (n.locationId === locationId) this.state.files.delete(id)
    }
    for (const key of [...this.state.children.keys()]) {
      if (key === this.state.rootKey(locationId)) this.state.children.delete(key)
    }
    for (const b of this.state.batches) {
      if (b.destLocationId === locationId || b.sourceLocationId === locationId) this.engine.cancelBatch(b.id)
    }
  }

  async refreshLocation(locationId: string): Promise<void> {
    const loc = this.state.locations.get(locationId)
    if (!loc) return
    await this.net(loc.kind, 400)
    runFakeCrawl(this.state, bus, locationId, Math.max(50, Math.round(loc.fileCount * 0.02)), 2200)
  }

  /* ---- files ---- */

  async listDir(locationId: string, dirId: string | null, opts?: Parameters<ApiClient['listDir']>[2]) {
    await this.net(this.state.locations.get(locationId)?.kind ?? 'localfs', 60)
    return this.state.listPage(locationId, dirId, opts ?? {})
  }

  async stat(_locationId: string, fileId: string) {
    await sleep(30)
    const n = this.state.node(fileId)
    return { ...n } as Awaited<ReturnType<ApiClient['stat']>>
  }

  async mkdir(locationId: string, parentId: string | null, name: string) {
    const kind = this.state.locations.get(locationId)?.kind ?? 'localfs'
    await this.net(kind, 160)
    if (this.state.siblings(locationId, parentId).some((n) => n.name.toLowerCase() === name.toLowerCase())) {
      throw new ApiError('conflict', `“${name}” already exists here`)
    }
    const dir = { ...makeDir(locationId, parentId, name, name), trashed: false }
    this.state.addChild(locationId, parentId, dir)
    this.touch(locationId, parentId)
    return { ...dir } as Awaited<ReturnType<ApiClient['mkdir']>>
  }

  async rename(locationId: string, fileId: string, newName: string) {
    const kind = this.state.locations.get(locationId)?.kind ?? 'localfs'
    await this.net(kind, 150)
    const n = this.state.node(fileId)
    if (this.state.siblings(locationId, n.parentId).some((s) => s.id !== fileId && s.name.toLowerCase() === newName.toLowerCase())) {
      throw new ApiError('conflict', `“${newName}” already exists here`)
    }
    n.name = newName
    n.etag = `${n.etag.slice(0, 8)}r`
    this.touch(locationId, n.parentId)
    return { ...n } as Awaited<ReturnType<ApiClient['rename']>>
  }

  async moveWithin(locationId: string, fileIds: string[], destDirId: string | null): Promise<void> {
    const kind = this.state.locations.get(locationId)?.kind ?? 'localfs'
    await this.net(kind, 220)
    const touchedOld = new Set<string | null>()
    for (const id of fileIds) {
      const n = this.state.node(id)
      const oldParent = n.parentId
      touchedOld.add(oldParent)
      const oldKey = oldParent ?? this.state.rootKey(locationId)
      const arr = this.state.children.get(oldKey)
      if (arr) {
        const i = arr.indexOf(id)
        if (i >= 0) arr.splice(i, 1)
      }
      n.name = this.state.dedupeName(locationId, destDirId, n.name)
      n.parentId = destDirId
      const key = destDirId ?? this.state.rootKey(locationId)
      const arr2 = this.state.children.get(key)
      if (arr2) arr2.push(id)
      else this.state.children.set(key, [id])
    }
    for (const d of touchedOld) this.touch(locationId, d)
    this.touch(locationId, destDirId)
  }

  async deleteFiles(locationId: string, fileIds: string[]): Promise<void> {
    const kind = this.state.locations.get(locationId)?.kind ?? 'localfs'
    await this.net(kind, 180)
    const touchedDirs = new Set<string | null>()
    for (const id of fileIds) {
      const n = this.state.node(id)
      touchedDirs.add(n.parentId)
      n.trashed = true
    }
    for (const d of touchedDirs) this.touch(locationId, d)
    this.refreshCounts(locationId)
    const names = fileIds.length === 1 ? `“${this.state.node(fileIds[0]).name}”` : `${fileIds.length} items`
    this.toast(`${names} moved to Trash`, undefined, 'default')
  }

  async download(locationId: string, fileIds: string[]): Promise<{ targets: string[] }> {
    if (fileIds.length === 0) return { targets: [] }
    const kind = this.state.locations.get(locationId)?.kind ?? 'localfs'
    await this.net(kind, 500 + Math.min(2500, fileIds.length * 120))
    const targets = fileIds.map((id) => {
      const n = this.state.node(id)
      return `${this.state.settings.downloadDir}/${n.name}`
    })
    const label = fileIds.length === 1 ? this.state.node(fileIds[0]).name : `${fileIds.length} files`
    this.toast(`Saved to ~/Downloads`, `${label} — ${Math.round(Math.random() * 300 + 60)} MB/s average`, 'success', 'Reveal')
    return { targets }
  }

  async uploadFiles(locationId: string, destDirId: string | null, files: { name: string; size: number }[]): Promise<void> {
    const kind = this.state.locations.get(locationId)?.kind ?? 'localfs'
    await this.net(kind, 120)
    const rng = new Rng(Date.now() & 0xffff)
    const specs = files.map((f) => {
      // placeholder node — invisible (trashed) until its transfer completes
      const node = { ...makeFile(locationId, destDirId, f.name, rng, 'upload'), trashed: true }
      node.size = f.size
      this.state.files.set(node.id, node)
      return { nodeId: node.id, name: f.name, size: f.size }
    })
    this.engine.enqueueUpload(locationId, destDirId, specs)
  }

  /* ---- transfers ---- */

  async enqueueTransfer(req: Parameters<ApiClient['enqueueTransfer']>[0]): Promise<{ batchId: string }> {
    if (req.fileIds.length === 0) throw new ApiError('empty', 'nothing selected')
    await sleep(80)
    const batchId = this.engine.enqueue(req)
    const destLoc = this.state.locations.get(req.destLocationId)
    const first = this.state.node(req.fileIds[0])
    const srcLoc = this.state.locations.get(first.locationId)
    this.toast(
      `${req.action === 'move' ? 'Moving' : 'Copying'} ${first.name}${req.fileIds.length > 1 ? ` +${req.fileIds.length - 1} more` : ''}`,
      `${srcLoc?.name ?? ''} → ${destLoc?.name ?? ''}`,
    )
    return { batchId }
  }

  async pauseBatch(batchId: string): Promise<void> {
    this.engine.pauseBatch(batchId)
  }

  async resumeBatch(batchId: string): Promise<void> {
    this.engine.resumeBatch(batchId)
  }

  async cancelBatch(batchId: string): Promise<void> {
    this.engine.cancelBatch(batchId)
  }

  async retryFailed(batchId: string): Promise<void> {
    this.engine.retryFailed(batchId)
  }

  async listBatches(): Promise<TransferBatch[]> {
    return this.state.batches.map((b) => ({ ...b }))
  }

  async listJobs(batchIds: string[]): Promise<TransferJob[]> {
    const set = new Set(batchIds)
    return this.state.jobs.filter((j) => set.has(j.batchId)).map((j) => ({ ...j }))
  }

  /* ---- search ---- */

  async search(q: Parameters<ApiClient['search']>[0]) {
    await sleep(60 + Math.min(240, q.text.length * 12))
    return this.state.search(q)
  }

  /* ---- settings ---- */

  async getSettings(): Promise<Settings> {
    await sleep(30)
    return { ...this.state.settings }
  }

  async updateSettings(patch: Partial<Settings>): Promise<Settings> {
    await sleep(80)
    Object.assign(this.state.settings, patch)
    return { ...this.state.settings }
  }
}

/* ------------------------------------------------------------------ */
/* Singleton + dev controls                                            */
/* ------------------------------------------------------------------ */

const client = new MockClient()

export const mockClient: ApiClient & EventBus & MockDevControls = Object.assign(client, {
  on: bus.on.bind(bus),
  /* dev */
  failNextCall(): void {
    client.failNext = true
    client.engine.failNext = true
  },
  addStressLocation(): void {
    if (client.state.locations.has('loc_localfs_9901')) return
    const loc = client.state.loadPreset(STRESS_PRESET, STRESS_PRESET.seed)
    runFakeCrawl(client.state, bus, loc.id, loc.fileCount, 4000)
  },
  reset(): void {
    client.state.locations.clear()
    client.state.files.clear()
    client.state.children.clear()
    client.state.batches = []
    client.state.jobs = []
    client.engine.stop()
    client.engine.start()
    client.seed()
  },
  clearAll(): void {
    client.state.locations.clear()
    client.state.files.clear()
    client.state.children.clear()
    client.state.batches = []
    client.state.jobs = []
  },
  setOffline(v: boolean): void {
    client.engine.offline = v
  },
  isOffline(): boolean {
    return client.engine.offline
  },
  setLatency(v: number): void {
    client.latencyMultiplier = v
  },
  getLatency(): number {
    return client.latencyMultiplier
  },
  hasStress(): boolean {
    return client.state.locations.has('loc_localfs_9901')
  },
})

/** category helper re-export so components can badge files */
export { fileMeta }
