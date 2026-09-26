import { useState } from 'react'
import {
  AlertCircle,
  ArrowRight,
  Check,
  CircleSlash,
  Copy,
  Move,
  Pause,
  RotateCw,
  Upload,
  X,
} from 'lucide-react'
import { useTransfersStore, type BatchView } from '@/stores/transfers'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { formatBytes, formatSpeed, formatEta } from '@/lib/format'
import { Progress } from '@/components/ui/progress'
import { Badge } from '@/components/ui/badge'
import { useAppStore } from '@/stores/app'

const ACTION_ICON = { copy: Copy, move: Move, upload: Upload }

function BatchRow({ batch }: { batch: BatchView }) {
  const [open, setOpen] = useState(false)
  const Icon = ACTION_ICON[batch.action]
  const progress = batch.bytesTotal > 0 ? batch.bytesDone / batch.bytesTotal : 0
  const srcLoc = useAppStore((s) => s.locations.find((l) => l.id === batch.sourceLocationId)?.name)
  const dstLoc = useAppStore((s) => s.locations.find((l) => l.id === batch.destLocationId)?.name)
  const eta = batch.speed > 0 ? formatEta(batch.bytesTotal - batch.bytesDone, batch.speed) : null

  return (
    <div className="border-b border-hairline last:border-b-0">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-white/[0.02]"
      >
        <span className={cn('flex size-7 shrink-0 items-center justify-center rounded-[6px] border', batch.active ? 'border-amber/30 bg-amber/10 text-amber' : 'border-hairline bg-white/[0.03] text-ink-faint')}>
          <Icon size={13} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="truncate text-[12.5px] font-medium text-ink">
              {batch.action === 'upload' ? 'Upload' : batch.action === 'move' ? 'Move' : 'Copy'} ·{' '}
              {batch.jobs.length} item{batch.jobs.length !== 1 ? 's' : ''}
            </span>
            {srcLoc && dstLoc && (
              <span className="hidden truncate font-mono text-[10.5px] text-ink-faint sm:flex sm:items-center sm:gap-1">
                {srcLoc} <ArrowRight size={9} className="shrink-0" /> {dstLoc}
              </span>
            )}
          </div>
          <div className="mt-1 flex items-center gap-2">
            <Progress
              value={progress}
              tone={batch.failedCount > 0 ? 'danger' : batch.active ? 'amber' : 'success'}
              className="max-w-[340px] flex-1"
            />
            <span className="shrink-0 font-mono text-[10.5px] tabular text-ink-faint">
              {batch.active
                ? `${formatBytes(batch.bytesDone)} / ${formatBytes(batch.bytesTotal)} · ${formatSpeed(batch.speed)}${eta ? ` · ${eta}` : ''}`
                : `${batch.doneCount} done · ${batch.skippedCount} skipped · ${batch.failedCount} failed · ${batch.cancelledCount} cancelled`}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {batch.active && (
            <>
              <span
                role="button"
                tabIndex={-1}
                title="Pause"
                onClick={(e) => {
                  e.stopPropagation()
                  void api.pauseBatch(batch.id)
                }}
                className="rounded p-1.5 text-ink-mut transition-colors hover:bg-white/10 hover:text-ink"
              >
                <Pause size={13} />
              </span>
              <span
                role="button"
                tabIndex={-1}
                title="Cancel"
                onClick={(e) => {
                  e.stopPropagation()
                  void api.cancelBatch(batch.id)
                }}
                className="rounded p-1.5 text-ink-mut transition-colors hover:bg-danger/15 hover:text-danger"
              >
                <X size={13} />
              </span>
            </>
          )}
          {!batch.active && batch.failedCount > 0 && (
            <span
              role="button"
              tabIndex={-1}
              title="Retry failed"
              onClick={(e) => {
                e.stopPropagation()
                void api.retryFailed(batch.id)
              }}
              className="rounded p-1.5 text-amber transition-colors hover:bg-amber/15"
            >
              <RotateCw size={13} />
            </span>
          )}
          {!batch.active && !batch.failedCount && (
            <Badge tone={batch.cancelledCount > 0 ? 'default' : 'success'}>
              {batch.cancelledCount > 0 ? <CircleSlash size={9} /> : <Check size={9} />}
              {batch.cancelledCount > 0 ? 'cancelled' : 'complete'}
            </Badge>
          )}
        </div>
      </button>

      {open && (
        <div className="max-h-[180px] overflow-y-auto border-t border-hairline bg-black/20">
          {batch.jobs.map((job) => (
            <div key={job.id} className="flex items-center gap-3 px-4 py-1.5 text-[11.5px]">
              <span className="w-3 shrink-0">
                {job.state === 'done' && <Check size={11} className="text-success" />}
                {job.state === 'failed' && <AlertCircle size={11} className="text-danger" />}
                {job.state === 'skipped' && <CircleSlash size={11} className="text-ink-faint" />}
                {job.state === 'running' && <span className="block size-[5px] rounded-full bg-amber" />}
                {job.state === 'pending' && <span className="block size-[5px] rounded-full bg-ink-faint" />}
                {job.state === 'paused' && <Pause size={10} className="text-ink-faint" />}
                {job.state === 'cancelled' && <X size={11} className="text-ink-faint" />}
              </span>
              <span className="min-w-0 flex-1 truncate text-ink-mut">
                {job.destPath ? `${job.destPath}/` : ''}
                {job.sourceName}
              </span>
              <span className="shrink-0 font-mono tabular text-[10px] text-ink-faint">
                {job.state === 'running'
                  ? `${formatBytes(job.bytesDone)} / ${formatBytes(job.bytesTotal)}`
                  : job.state === 'failed'
                    ? (job.error ?? 'failed')
                    : job.state === 'skipped'
                      ? 'identical — skipped'
                      : job.state === 'done'
                        ? `${formatBytes(job.bytesTotal)} · md5 ✓`
                        : formatBytes(job.bytesTotal)}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export function TransferDrawer() {
  const batches = useTransfersStore((s) => s.batches)
  const open = useTransfersStore((s) => s.drawerOpen)
  const setOpen = useTransfersStore((s) => s.setDrawerOpen)

  const active = batches.filter((b) => b.active)
  const totalSpeed = active.reduce((a, b) => a + b.speed, 0)
  const overallBytes = active.reduce((a, b) => a + b.bytesDone, 0)
  const overallTotal = active.reduce((a, b) => a + b.bytesTotal, 0)

  if (batches.length === 0 && !open) return null

  return (
    <div
      className={cn(
        'pointer-events-auto absolute inset-x-0 bottom-0 z-40 border-t border-hairline bg-panel/95 backdrop-blur',
        'shadow-[0_-12px_40px_-12px_rgba(0,0,0,0.6)]',
      )}
    >
      {!open ? (
        <button
          onClick={() => setOpen(true)}
          className="flex h-9 w-full items-center gap-3 px-4 text-left"
        >
          <span className="size-[6px] animate-pulse rounded-full bg-amber" />
          <span className="text-[12.5px] text-ink">
            {active.length} active transfer{active.length !== 1 ? 's' : ''}
          </span>
          <span className="font-mono text-[10.5px] tabular text-ink-faint">
            {formatBytes(overallBytes)} / {formatBytes(overallTotal)} · {formatSpeed(totalSpeed)}
          </span>
          <Progress value={overallTotal > 0 ? overallBytes / overallTotal : 0} className="ml-auto max-w-[240px]" />
        </button>
      ) : (
        <div className="animate-slide-up">
          <div className="flex h-9 items-center gap-3 border-b border-hairline px-4">
            <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-ink-faint">transfers</span>
            {active.length > 0 && (
              <span className="font-mono text-[10.5px] tabular text-amber">{formatSpeed(totalSpeed)}</span>
            )}
            <button
              onClick={() => setOpen(false)}
              className="ml-auto rounded p-1 text-ink-faint transition-colors hover:bg-white/10 hover:text-ink"
            >
              <X size={13} />
            </button>
          </div>
          <div className="max-h-[300px] overflow-y-auto">
            {batches.map((b) => (
              <BatchRow key={b.id} batch={b} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
