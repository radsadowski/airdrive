/**
 * THE CONTRACT — AirDrive API spec.
 *
 * The frontend only ever talks to `ApiClient` + `EventBus` (ports/adapters).
 * Two implementations exist:
 *   - `lib/api/mock/client.ts`   — stateful in-memory app, runs in a plain browser
 *   - `lib/api/wails/client.ts` — wraps generated Wails v3 bindings + Events.On
 *
 * Types here mirror the Go bridge 1:1. Drift between the two clients is a build
 * error, not a bug report.
 */

/* ------------------------------------------------------------------ */
/* Locations                                                            */
/* ------------------------------------------------------------------ */

export type ProviderKind = 'localfs' | 'gdrive' | 's3'

export type LocationStatus =
  | 'connecting'
  | 'authorizing'
  | 'indexing'
  | 'syncing'
  | 'idle'
  | 'error'
  | 'offline'

export interface Location {
  id: string
  kind: ProviderKind
  name: string
  /** folder path / bucket name / "My Drive" */
  root: string
  status: LocationStatus
  /** number of indexed files (dirs excluded) */
  fileCount: number
  /** total bytes indexed */
  totalSize: number
  lastSyncedAt: string | null
  error?: string
}

export interface S3LocationConfig {
  preset: 'r2' | 'b2' | 'minio' | 'aws'
  endpoint: string
  region: string
  accessKeyId: string
  secretAccessKey: string
  bucket: string
  pathStyle: boolean
}

/** phase 1 of the Drive connect flow — sign-in result, valid ~10 minutes */
export interface GdriveSession {
  sessionId: string
  email: string
  name: string
}

/* ------------------------------------------------------------------ */
/* Files                                                                */
/* ------------------------------------------------------------------ */

export type FileKind = 'file' | 'dir'

export type PreviewKind = 'image' | 'text' | 'pdf' | 'video' | 'audio' | 'binary'

export interface FileInfo {
  id: string
  locationId: string
  parentId: string | null
  name: string
  kind: FileKind
  size: number
  mime: string
  modifiedAt: string
  /** rev (Drive) / ETag (S3) / modtime+size (local) — the change-detection anchor */
  etag: string
  previewKind?: PreviewKind
  /** images only — real: served by the Go asset middleware at /thumb/{locationId}/{fileId}; mock: seeded picsum URLs */
  thumbnailUrl?: string
}

export type SortField = 'name' | 'size' | 'modifiedAt' | 'kind'
export type SortDir = 'asc' | 'desc'

export interface SortSpec {
  field: SortField
  dir: SortDir
}

export interface Page<T> {
  items: T[]
  /** opaque cursor for the next page; null when exhausted */
  cursor: string | null
}

export interface ListDirOptions {
  sort?: SortSpec
  cursor?: string
  limit?: number
  includeHidden?: boolean
}

/* ------------------------------------------------------------------ */
/* Search                                                               */
/* ------------------------------------------------------------------ */

export type FileCategory =
  | 'image'
  | 'video'
  | 'audio'
  | 'document'
  | 'pdf'
  | 'archive'
  | 'code'
  | 'other'

export interface SearchQuery {
  text: string
  categories?: FileCategory[]
  locationIds?: string[]
  sizeMin?: number
  sizeMax?: number
  modifiedFrom?: string
  modifiedTo?: string
  cursor?: string
  limit?: number
}

/* ------------------------------------------------------------------ */
/* Transfers                                                            */
/* ------------------------------------------------------------------ */

export type TransferState =
  | 'pending'
  | 'running'
  | 'done'
  | 'failed'
  | 'skipped'
  | 'cancelled'
  | 'paused'

export interface TransferJob {
  id: string
  batchId: string
  sourceFileId: string
  sourceName: string
  sourceLocationId: string
  destLocationId: string
  destPath: string
  state: TransferState
  bytesTotal: number
  bytesDone: number
  speed: number
  md5Verified: boolean
  error?: string
}

export interface TransferBatch {
  id: string
  action: 'copy' | 'move' | 'upload'
  createdAt: string
  sourceLocationId: string
  destLocationId: string
  /** destination directory the user picked (null = location root) */
  destDirId: string | null
  destName: string
  conflictPolicy: ConflictPolicy
  deleteSource: boolean
  jobIds: string[]
}

export type ConflictPolicy = 'smart' | 'overwrite' | 'skip'

export interface EnqueueRequest {
  action: 'copy' | 'move'
  fileIds: string[]
  destLocationId: string
  destDirId: string | null
  conflictPolicy: ConflictPolicy
}

/* ------------------------------------------------------------------ */
/* Settings                                                             */
/* ------------------------------------------------------------------ */

export interface Settings {
  theme: 'dark' | 'light'
  defaultView: 'list' | 'grid'
  concurrentTransfers: number
  bandwidthLimitMbps: number
  showHiddenFiles: boolean
  downloadDir: string
}

/* ------------------------------------------------------------------ */
/* Events (Go → UI, via app.Event.Emit / mock equivalent)               */
/* ------------------------------------------------------------------ */

export interface ToastPayload {
  id: string
  title: string
  description?: string
  variant?: 'default' | 'success' | 'error'
  actionLabel?: string
  /** token used by the UI to invoke the toast action */
  actionToken?: string
}

export interface AppEventMap {
  'index:progress': { locationId: string; filesDone: number; filesTotal: number }
  'fs:changed': { locationId: string; dirId: string | null }
  'transfer:updated': {
    batchId: string
    jobId: string
    state: TransferState
    bytesDone: number
    bytesTotal: number
    speed: number
  }
  'transfer:batchDone': { batchId: string; done: number; skipped: number; failed: number }
  'location:status': { locationId: string; state: LocationStatus; error?: string }
  'toast:show': ToastPayload
}

export type AppEventName = keyof AppEventMap

export interface EventBus {
  on<K extends AppEventName>(name: K, cb: (payload: AppEventMap[K]) => void): () => void
}

/* ------------------------------------------------------------------ */
/* The client — mirrors the Wails bridge services 1:1                   */
/* ------------------------------------------------------------------ */

export interface AddLocalRequest {
  /** chosen root folder path (native picker under Wails) */
  root: string
  name?: string
}

export interface ApiClient {
  /* ---- locations ---- */
  listLocations(): Promise<Location[]>
  addLocalLocation(req: AddLocalRequest): Promise<Location>
  /** phase 1: full interactive Google sign-in (system browser, PKCE, loopback) */
  connectGdrive(): Promise<GdriveSession>
  /** phase 2a: folder-only listing for the picker (dirId null = My Drive root) */
  browseGdriveFolders(sessionId: string, dirId: string | null): Promise<FileInfo[]>
  /** starred folders account-wide — the only API path to Drive-for-desktop "Computers" folders */
  starredGdriveFolders(sessionId: string): Promise<FileInfo[]>
  /** phase 2b: create the location rooted at the picked folder (null = whole Drive) */
  confirmGdriveLocation(sessionId: string, folderId: string | null): Promise<Location>
  addS3Location(cfg: S3LocationConfig): Promise<Location>
  removeLocation(locationId: string): Promise<void>
  refreshLocation(locationId: string): Promise<void>
  /** used by the Drive / S3 wizards to validate a connection before adding */
  testS3Connection(cfg: S3LocationConfig): Promise<{ ok: boolean; buckets: string[]; error?: string }>

  /* ---- files ---- */
  listDir(locationId: string, dirId: string | null, opts?: ListDirOptions): Promise<Page<FileInfo>>
  stat(locationId: string, fileId: string): Promise<FileInfo>
  mkdir(locationId: string, parentId: string | null, name: string): Promise<FileInfo>
  rename(locationId: string, fileId: string, newName: string): Promise<FileInfo>
  /** same-provider move (cheap, native on Drive / local) */
  moveWithin(locationId: string, fileIds: string[], destDirId: string | null): Promise<void>
  /** to provider trash; never permanent */
  deleteFiles(locationId: string, fileIds: string[]): Promise<void>
  /** one-click download → ~/Downloads; returns resolved target path */
  download(locationId: string, fileIds: string[]): Promise<{ targets: string[] }>
  /** host-OS drop / picker upload into a directory */
  uploadFiles(locationId: string, destDirId: string | null, files: { name: string; size: number }[]): Promise<void>

  /* ---- transfers ---- */
  enqueueTransfer(req: EnqueueRequest): Promise<{ batchId: string }>
  pauseBatch(batchId: string): Promise<void>
  resumeBatch(batchId: string): Promise<void>
  cancelBatch(batchId: string): Promise<void>
  retryFailed(batchId: string): Promise<void>
  listBatches(): Promise<TransferBatch[]>
  listJobs(batchIds: string[]): Promise<TransferJob[]>

  /* ---- search ---- */
  search(q: SearchQuery): Promise<Page<FileInfo>>

  /* ---- settings ---- */
  getSettings(): Promise<Settings>
  updateSettings(patch: Partial<Settings>): Promise<Settings>
}
