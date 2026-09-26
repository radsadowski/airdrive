/**
 * WailsClient — implements ApiClient + EventBus using the generated Wails v3 bindings.
 * This is the only file that imports @wailsio/runtime. Phase 2+ swap mock→real
 * one vertical slice at a time by routing calls here instead of MockClient.
 */

import { Events } from '@wailsio/runtime'
import type { ApiClient, AppEventMap, AppEventName, EventBus, FileInfo, GdriveSession, Location, Page, S3LocationConfig, Settings } from '../contract'
import * as LocSvc from '@bindings/github.com/radosny/airdrive/internal/bridge/locationservice'
import * as FileSvc from '@bindings/github.com/radosny/airdrive/internal/bridge/fileservice'
import type { File as EntityFile, Location as EntityLocation, Page as EntityPage } from '@bindings/github.com/radosny/airdrive/internal/entities/models'

// ---- shape adapters ------------------------------------------------------ //

/** Go pointer returns serialize as T | null. Throw if the server returned null (shouldn't happen on success paths). */
function requireNonNull<T>(val: T | null, name: string): T {
  if (val === null) throw new Error(`${name}: unexpected null response from backend`)
  return val
}

function toLocation(loc: EntityLocation): Location {
  return {
    id: loc.id,
    kind: loc.kind as Location['kind'],
    name: loc.name,
    root: loc.root,
    status: (loc.status as Location['status']) || 'idle',
    fileCount: loc.fileCount,
    totalSize: loc.totalSize,
    lastSyncedAt: loc.lastSyncedAt || null,
    error: loc.error,
  }
}

function toFile(f: EntityFile): FileInfo {
  return {
    id: f.id,
    locationId: f.locationId,
    parentId: f.parentId || null,
    name: f.name,
    kind: f.kind as FileInfo['kind'],
    size: f.size,
    mime: f.mime,
    modifiedAt: f.modifiedAt,
    etag: f.etag,
    previewKind: f.previewKind as FileInfo['previewKind'],
    thumbnailUrl:
      f.previewKind === 'image'
        ? `/thumb/${f.locationId}/${encodeURIComponent(f.id)}?v=${encodeURIComponent(f.etag)}`
        : undefined,
  }
}

function toPage(p: EntityPage): Page<FileInfo> {
  return {
    items: (p.items ?? []).map(toFile),
    cursor: p.cursor || null,
  }
}

// ---- WailsClient --------------------------------------------------------- //

class WailsClientImpl implements ApiClient, EventBus {
  // ---- EventBus ---- //

  on<K extends AppEventName>(name: K, cb: (payload: AppEventMap[K]) => void): () => void {
    return Events.On(name, (event: { data?: unknown[] }) => {
      const payload = Array.isArray(event.data) ? event.data[0] : event.data
      cb(payload as AppEventMap[K])
    })
  }

  // ---- Locations ---- //

  async listLocations(): Promise<Location[]> {
    const locs = await LocSvc.ListLocations()
    return (locs ?? []).map(toLocation)
  }

  async addLocalLocation(req: { root: string; name?: string }): Promise<Location> {
    const loc = await LocSvc.AddLocalLocation(req.root, req.name ?? '')
    return toLocation(requireNonNull(loc, 'addLocalLocation'))
  }

  async connectGdrive(): Promise<GdriveSession> {
    const sess = await LocSvc.ConnectGdrive()
    return requireNonNull(sess, 'connectGdrive')
  }

  async browseGdriveFolders(sessionId: string, dirId: string | null): Promise<FileInfo[]> {
    const folders = await LocSvc.BrowseGdriveFolders(sessionId, dirId ?? '')
    return (folders ?? []).map(toFile)
  }

  async starredGdriveFolders(sessionId: string): Promise<FileInfo[]> {
    const folders = await LocSvc.StarredGdriveFolders(sessionId)
    return (folders ?? []).map(toFile)
  }

  async confirmGdriveLocation(sessionId: string, folderId: string | null): Promise<Location> {
    const loc = await LocSvc.ConfirmGdriveLocation(sessionId, folderId ?? '')
    return toLocation(requireNonNull(loc, 'confirmGdriveLocation'))
  }

  async addS3Location(cfg: S3LocationConfig): Promise<Location> {
    const loc = await LocSvc.AddS3Location(cfg)
    return toLocation(requireNonNull(loc, 'addS3Location'))
  }

  async removeLocation(locationId: string): Promise<void> {
    await LocSvc.RemoveLocation(locationId)
  }

  async refreshLocation(locationId: string): Promise<void> {
    await LocSvc.RefreshLocation(locationId)
  }

  async testS3Connection(cfg: S3LocationConfig): Promise<{ ok: boolean; buckets: string[]; error?: string }> {
    const res = await LocSvc.TestS3Connection(cfg)
    const r = requireNonNull(res, 'testS3Connection')
    return { ok: r.ok, buckets: r.buckets ?? [], error: r.error || undefined }
  }

  // ---- Files ---- //

  async listDir(locationId: string, dirId: string | null, opts?: Parameters<ApiClient['listDir']>[2]): Promise<Page<FileInfo>> {
    const page = await FileSvc.ListDir(locationId, dirId ?? '', {
      sortField: opts?.sort?.field,
      sortAsc: opts?.sort?.dir === 'asc',
      cursor: opts?.cursor,
      limit: opts?.limit,
      includeHidden: opts?.includeHidden,
    })
    return toPage(requireNonNull(page, 'listDir'))
  }

  async stat(locationId: string, fileId: string): Promise<FileInfo> {
    const f = await FileSvc.Stat(locationId, fileId)
    return toFile(requireNonNull(f, 'stat'))
  }

  async mkdir(locationId: string, parentId: string | null, name: string): Promise<FileInfo> {
    const f = await FileSvc.Mkdir(locationId, parentId ?? '', name)
    return toFile(requireNonNull(f, 'mkdir'))
  }

  async rename(locationId: string, fileId: string, newName: string): Promise<FileInfo> {
    const f = await FileSvc.Rename(locationId, fileId, newName)
    return toFile(requireNonNull(f, 'rename'))
  }

  async moveWithin(locationId: string, fileIds: string[], destDirId: string | null): Promise<void> {
    await FileSvc.MoveWithin(locationId, fileIds, destDirId ?? '')
  }

  async deleteFiles(locationId: string, fileIds: string[]): Promise<void> {
    await FileSvc.DeleteFiles(locationId, fileIds)
  }

  async download(locationId: string, fileIds: string[]): Promise<{ targets: string[] }> {
    const targets = await FileSvc.Download(locationId, fileIds)
    return { targets: targets ?? [] }
  }

  async uploadFiles(_locationId: string, _destDirId: string | null, _files: { name: string; size: number }[]): Promise<void> {
    // Phase 6: real upload via transfer manager
    throw new Error('Upload — coming in Phase 6')
  }

  // ---- Transfers (Phase 6) ---- //

  async enqueueTransfer(): Promise<{ batchId: string }> {
    throw new Error('Transfer manager — coming in Phase 6')
  }
  async pauseBatch(): Promise<void> { throw new Error('Phase 6') }
  async resumeBatch(): Promise<void> { throw new Error('Phase 6') }
  async cancelBatch(): Promise<void> { throw new Error('Phase 6') }
  async retryFailed(): Promise<void> { throw new Error('Phase 6') }
  async listBatches() { return [] }
  async listJobs() { return [] }

  // ---- Search ---- //

  async search(q: Parameters<ApiClient['search']>[0]): Promise<Page<FileInfo>> {
    const page = await FileSvc.Search(
      q.text,
      q.locationIds ?? [],
      q.sizeMin ?? 0,
      q.sizeMax ?? 0,
      q.modifiedFrom ?? '',
      q.modifiedTo ?? '',
      '',
      q.limit ?? 150,
      q.cursor ?? '',
    )
    return toPage(requireNonNull(page, 'search'))
  }

  // ---- Settings (Phase 2: in-memory only, persisted in Phase 7) ---- //

  private settings: Settings = {
    theme: 'dark',
    defaultView: 'list',
    concurrentTransfers: 3,
    bandwidthLimitMbps: 80,
    showHiddenFiles: false,
    downloadDir: `${window.location.hostname}/Downloads`,
  }

  async getSettings(): Promise<Settings> {
    return { ...this.settings }
  }

  async updateSettings(patch: Partial<Settings>): Promise<Settings> {
    Object.assign(this.settings, patch)
    return { ...this.settings }
  }
}

export const wailsClient = new WailsClientImpl()
