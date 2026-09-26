/**
 * TransferEngine — the migration engine, mocked.
 * Mirrors the Go design: journaled per-file jobs, worker slots, conflict
 * resolution at job start, "delete source after verified copy" for moves.
 */

import type { ConflictPolicy, EnqueueRequest, TransferBatch, TransferJob } from '../contract'
import { nextId } from './data'
import type { MockState } from './state'
import type { bus } from './bus'

export interface TransferEngineDeps {
  state: MockState
  bus: typeof bus
}

const TICK_MS = 350

export class TransferEngine {
  private state: MockState
  private bus: TransferEngineDeps['bus']
  private timer: ReturnType<typeof setInterval> | null = null
  private pausedBatches = new Set<string>()
  private cancelledBatches = new Set<string>()
  /** offline mode — running jobs fail with a retryable network error */
  offline = false
  /** arm a one-shot transfer failure for the next started job */
  failNext = false

  constructor(deps: TransferEngineDeps) {
    this.state = deps.state
    this.bus = deps.bus
  }

  start(): void {
    if (this.timer) return
    this.timer = setInterval(() => this.tick(), TICK_MS)
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer)
    this.timer = null
  }

  enqueue(req: EnqueueRequest): string {
    const { state } = this
    const batchId = `batch_${nextId('b')}`
    const expansions = state.expandSelection(req.fileIds)
    const jobs: TransferJob[] = expansions.map(({ fileId, relPath }) => {
      const src = state.node(fileId)
      return {
        id: `job_${nextId('j')}`,
        batchId,
        sourceFileId: fileId,
        sourceName: src.name,
        sourceLocationId: src.locationId,
        destLocationId: req.destLocationId,
        destPath: relPath.join('/'),
        state: 'pending' as const,
        bytesTotal: src.size,
        bytesDone: 0,
        speed: 0,
        md5Verified: false,
      }
    })
    const batch: TransferBatch = {
      id: batchId,
      action: req.action,
      createdAt: new Date().toISOString(),
      sourceLocationId: jobs[0]?.sourceLocationId ?? '',
      destLocationId: req.destLocationId,
      destDirId: req.destDirId,
      destName: '',
      conflictPolicy: req.conflictPolicy,
      deleteSource: req.action === 'move',
      jobIds: jobs.map((j) => j.id),
    }
    state.batches.unshift(batch)
    state.jobs.unshift(...jobs)
    return batchId
  }

  uploadDestDirs = new Map<string, string | null>()

  enqueueUpload(
    locationId: string,
    dirId: string | null,
    files: { nodeId: string; name: string; size: number }[],
  ): string {
    const { state } = this
    const batchId = `batch_${nextId('b')}`
    const jobs: TransferJob[] = files.map((f) => ({
      id: `job_${nextId('j')}`,
      batchId,
      sourceFileId: f.nodeId,
      sourceName: f.name,
      sourceLocationId: locationId,
      destLocationId: locationId,
      destPath: '',
      state: 'pending' as const,
      bytesTotal: f.size,
      bytesDone: 0,
      speed: 0,
      md5Verified: false,
    }))
    for (const j of jobs) this.uploadDestDirs.set(j.id, dirId)
    const batch: TransferBatch = {
      id: batchId,
      action: 'upload',
      createdAt: new Date().toISOString(),
      sourceLocationId: locationId,
      destLocationId: locationId,
      destDirId: dirId,
      destName: '',
      conflictPolicy: 'smart' as ConflictPolicy,
      deleteSource: false,
      jobIds: jobs.map((j) => j.id),
    }
    state.batches.unshift(batch)
    state.jobs.unshift(...jobs)
    return batchId
  }

  pauseBatch(batchId: string): void {
    this.pausedBatches.add(batchId)
    for (const j of this.state.jobs) {
      if (j.batchId === batchId && j.state === 'running') {
        j.state = 'paused'
        j.speed = 0
        this.emitJob(j)
      }
    }
  }

  resumeBatch(batchId: string): void {
    this.pausedBatches.delete(batchId)
    for (const j of this.state.jobs) {
      if (j.batchId === batchId && j.state === 'paused') {
        j.state = 'pending'
        this.emitJob(j)
      }
    }
  }

  cancelBatch(batchId: string): void {
    this.cancelledBatches.add(batchId)
    for (const j of this.state.jobs) {
      if (j.batchId === batchId && (j.state === 'pending' || j.state === 'running' || j.state === 'paused')) {
        j.state = 'cancelled'
        j.speed = 0
        this.emitJob(j)
      }
    }
  }

  retryFailed(batchId: string): void {
    for (const j of this.state.jobs) {
      if (j.batchId === batchId && j.state === 'failed') {
        j.state = 'pending'
        j.bytesDone = 0
        j.error = undefined
        this.emitJob(j)
      }
    }
  }

  jobsOf(batchId: string): TransferJob[] {
    return this.state.jobs.filter((j) => j.batchId === batchId)
  }

  private emitJob(j: TransferJob): void {
    this.bus.emit('transfer:updated', {
      batchId: j.batchId,
      jobId: j.id,
      state: j.state,
      bytesDone: j.bytesDone,
      bytesTotal: j.bytesTotal,
      speed: j.speed,
    })
  }

  private tick(): void {
    const { state } = this
    const limitBps = Math.max(1, state.settings.bandwidthLimitMbps) * 1_000_000 / 8
    const maxConcurrent = state.settings.concurrentTransfers

    const running = state.jobs.filter((j) => j.state === 'running')
    // promote pending → running (respect per-batch pause/cancel)
    for (const j of state.jobs) {
      if (running.length >= maxConcurrent) break
      if (j.state !== 'pending') continue
      if (this.pausedBatches.has(j.batchId) || this.cancelledBatches.has(j.batchId)) continue
      if (this.startJob(j)) running.push(j)
    }

    if (this.offline) {
      for (const j of running) {
        j.state = 'failed'
        j.error = 'network unreachable — will retry when online'
        j.speed = 0
        this.emitJob(j)
      }
      return
    }

    if (running.length === 0) {
      this.checkBatches()
      return
    }

    const perJob = limitBps / running.length
    for (const j of running) {
      const jitter = 0.6 + Math.random() * 0.8
      j.speed = Math.max(16_384, perJob * jitter)
      j.bytesDone = Math.min(j.bytesTotal, j.bytesDone + Math.round(j.speed * (TICK_MS / 1000)))
      if (j.bytesDone >= j.bytesTotal) {
        this.completeJob(j)
      } else {
        this.emitJob(j)
      }
    }
    this.checkBatches()
  }

  /** returns true when the job actually started */
  private startJob(j: TransferJob): boolean {
    if (this.failNext) {
      this.failNext = false
      j.state = 'failed'
      j.error = 'provider 500 (injected via dev panel)'
      this.emitJob(j)
      return false
    }
    j.state = 'running'
    this.emitJob(j)
    return true
  }

  private completeJob(j: TransferJob): void {
    const { state } = this
    const batch = state.batches.find((b) => b.id === j.batchId)
    if (!batch || this.cancelledBatches.has(j.batchId)) {
      j.state = 'cancelled'
      this.emitJob(j)
      return
    }

    // materialize the destination node (upload jobs carry placeholder sources)
    if (batch.action === 'upload') {
      this.materializeUpload(j)
    } else {
      this.materializeCopy(j, batch)
    }

    j.state = 'done'
    j.md5Verified = true
    j.speed = 0
    this.emitJob(j)

    // move semantics: delete source only after verified copy
    if (batch.deleteSource && j.sourceFileId) {
      const src = state.files.get(j.sourceFileId)
      if (src && !src.trashed) {
        src.trashed = true
        this.bus.emit('fs:changed', { locationId: src.locationId, dirId: src.parentId })
      }
    }
  }

  private materializeUpload(j: TransferJob): void {
    const { state } = this
    const node = state.files.get(j.sourceFileId)
    const destDirId = this.uploadDestDirs.get(j.id) ?? null
    this.uploadDestDirs.delete(j.id)
    if (!node) return
    if (destDirId === null || state.files.has(destDirId)) {
      node.trashed = false
      node.parentId = destDirId
      const key = destDirId ?? state.rootKey(node.locationId)
      const arr = state.children.get(key)
      if (arr && !arr.includes(node.id)) arr.push(node.id)
      this.bus.emit('fs:changed', { locationId: node.locationId, dirId: destDirId })
      const loc = state.locations.get(node.locationId)
      if (loc) {
        const { fileCount, totalSize } = state.countsFor(node.locationId)
        loc.fileCount = fileCount
        loc.totalSize = totalSize
      }
    }
  }

  /** create the copied node in the destination tree with conflict policy */
  private materializeCopy(j: TransferJob, batch: TransferBatch): void {
    const { state } = this
    const src = state.files.get(j.sourceFileId)
    if (!src) {
      j.state = 'failed'
      j.error = 'source vanished'
      return
    }
    const segments = j.destPath ? j.destPath.split('/') : []
    const destDirId = state.ensureDirPath(j.destLocationId, batch.destDirId, segments)
    const existing = state
      .siblings(j.destLocationId, destDirId)
      .find((n) => n.name.toLowerCase() === src.name.toLowerCase())

    if (existing && existing.kind === 'file') {
      if (batch.conflictPolicy === 'skip') {
        j.state = 'skipped'
        this.emitJob(j)
        return
      }
      if (batch.conflictPolicy === 'smart' && existing.etag === src.etag) {
        j.state = 'skipped'
        this.emitJob(j)
        return
      }
      if (batch.conflictPolicy === 'overwrite') {
        existing.trashed = true
      }
    }

    const name =
      existing && batch.conflictPolicy === 'smart'
        ? state.dedupeName(j.destLocationId, destDirId, src.name)
        : src.name

    const destNode = { ...src, id: nextId('f'), parentId: destDirId, name, modifiedAt: new Date().toISOString(), trashed: false }
    state.addChild(j.destLocationId, destDirId, destNode)
    this.bus.emit('fs:changed', { locationId: j.destLocationId, dirId: destDirId })

    const loc = state.locations.get(j.destLocationId)
    if (loc) {
      const { fileCount, totalSize } = state.countsFor(j.destLocationId)
      loc.fileCount = fileCount
      loc.totalSize = totalSize
    }
  }

  private checkBatches(): void {
    for (const b of this.state.batches) {
      if ((b as TransferBatch & { doneEmitted?: boolean }).doneEmitted) continue
      const jobs = this.jobsOf(b.id)
      if (jobs.length === 0) continue
      const terminal = jobs.every((j) => ['done', 'failed', 'skipped', 'cancelled'].includes(j.state))
      if (!terminal) continue
      ;(b as TransferBatch & { doneEmitted?: boolean }).doneEmitted = true
      this.bus.emit('transfer:batchDone', {
        batchId: b.id,
        done: jobs.filter((j) => j.state === 'done').length,
        skipped: jobs.filter((j) => j.state === 'skipped').length,
        failed: jobs.filter((j) => j.state === 'failed').length,
      })
    }
  }
}

/* ------------------------------------------------------------------ */
/* Crawl engine — fake index:progress over a few seconds                */
/* ------------------------------------------------------------------ */

export function runFakeCrawl(
  state: MockState,
  busInstance: TransferEngineDeps['bus'],
  locationId: string,
  fileTotal: number,
  durationMs?: number,
): void {
  const loc = state.locations.get(locationId)
  if (!loc) return
  loc.status = 'indexing'
  busInstance.emit('location:status', { locationId, state: 'indexing' })

  const duration = durationMs ?? Math.min(9000, 1600 + fileTotal / 400)
  const started = Date.now()
  const step = () => {
    const elapsed = Date.now() - started
    const t = Math.min(1, elapsed / duration)
    const eased = 1 - (1 - t) ** 2.2
    const filesDone = Math.round(eased * fileTotal)
    busInstance.emit('index:progress', { locationId, filesDone, filesTotal: fileTotal })
    if (t < 1) {
      setTimeout(step, 140 + Math.random() * 120)
    } else {
      const { fileCount, totalSize } = state.countsFor(locationId)
      loc.status = 'idle'
      loc.fileCount = fileCount
      loc.totalSize = totalSize
      loc.lastSyncedAt = new Date().toISOString()
      busInstance.emit('location:status', { locationId, state: 'idle' })
      busInstance.emit('fs:changed', { locationId, dirId: null })
    }
  }
  step()
}
