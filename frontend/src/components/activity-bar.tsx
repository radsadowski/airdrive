import { ArrowLeftRight, Copy, Loader2, Move, RefreshCw, Trash2, Upload } from 'lucide-react'
import { useAppStore } from '@/stores/app'
import { useSyncStore } from '@/stores/sync'
import { useTransfersStore } from '@/stores/transfers'
import { useOpsStore } from '@/stores/ops'
import { useUiStore } from '@/stores/ui'
import { Progress } from '@/components/ui/progress'
import { formatBytes, formatSpeed } from '@/lib/format'
import { cn } from '@/lib/utils'

type Tone = 'amber' | 'success' | 'danger'

interface BarItem {
  key: string
  label: string
  detail: string
  value: number
  indeterminate?: boolean
  tone?: Tone
  icon: React.ReactNode
  onClick?: () => void
}

const TRANSFER_LABEL = { copy: 'Copying', move: 'Moving', upload: 'Uploading' }
const TRANSFER_ICON = { copy: Copy, move: Move, upload: Upload }

/**
 * ActivityBar: one strip for every operation in flight. Indexing, syncing,
 * transfers and file operations (trash, in-place moves) report here, so a
 * delete shows progress exactly like a copy or a sync pass.
 */
export function ActivityBar() {
  const crawlProgress = useAppStore((s) => s.crawlProgress)
  const locations = useAppStore((s) => s.locations)
  const runs = useSyncStore((s) => s.activeRuns)
  const batches = useTransfersStore((s) => s.batches)
  const setDrawerOpen = useTransfersStore((s) => s.setDrawerOpen)
  const ops = useOpsStore((s) => s.ops)
  const setSyncOpen = useUiStore((s) => s.setSyncOpen)

  const items: BarItem[] = []

  for (const [locationId, crawl] of Object.entries(crawlProgress)) {
    const name = locations.find((l) => l.id === locationId)?.name ?? 'location'
    items.push({
      key: `index:${locationId}`,
      label: `Indexing ${name}`,
      detail: crawl.total > 0 ? `${crawl.done}/${crawl.total}` : `${crawl.done} files`,
      value: crawl.total > 0 ? crawl.done / crawl.total : 0,
      indeterminate: crawl.total === 0,
      icon: <RefreshCw size={11} />,
    })
  }

  for (const r of Object.values(runs)) {
    items.push({
      key: `sync:${r.pairId}`,
      label: r.name || 'Syncing',
      detail: `${r.filesTotal > 0 ? `${r.filesDone}/${r.filesTotal}` : '…'} · ${formatBytes(r.bytesDone)}${r.speed > 0 ? ` · ${formatSpeed(r.speed)}` : ''}`,
      value: r.bytesTotal > 0 ? r.bytesDone / r.bytesTotal : 0,
      indeterminate: r.bytesTotal === 0 && r.filesTotal === 0,
      icon: <ArrowLeftRight size={11} />,
      onClick: () => setSyncOpen(true),
    })
  }

  const activeBatches = batches.filter((b) => b.active)
  if (activeBatches.length > 0) {
    const jobs = activeBatches.reduce((a, b) => a + b.jobs.length, 0)
    const done = activeBatches.reduce((a, b) => a + b.doneCount, 0)
    const bytesDone = activeBatches.reduce((a, b) => a + b.bytesDone, 0)
    const bytesTotal = activeBatches.reduce((a, b) => a + b.bytesTotal, 0)
    const speed = activeBatches.reduce((a, b) => a + b.speed, 0)
    const single = activeBatches.length === 1 ? activeBatches[0] : null
    const Icon = single ? TRANSFER_ICON[single.action] : Copy
    const running = activeBatches.flatMap((b) => b.jobs).find((j) => j.state === 'running')
    items.push({
      key: 'transfer',
      label: single
        ? `${TRANSFER_LABEL[single.action]} ${single.jobs.length} item${single.jobs.length !== 1 ? 's' : ''}${running ? ` · ${running.destPath || running.sourceName}` : ''}`
        : `${activeBatches.length} transfers`,
      detail: `${bytesTotal > 0 ? `${formatBytes(bytesDone)} / ${formatBytes(bytesTotal)}` : `${done}/${jobs}`}${speed > 0 ? ` · ${formatSpeed(speed)}` : ''}`,
      value: bytesTotal > 0 ? bytesDone / bytesTotal : jobs > 0 ? done / jobs : 0,
      icon: <Icon size={11} />,
      onClick: () => setDrawerOpen(true),
    })
  }

  for (const op of Object.values(ops)) {
    const verb = op.op === 'delete' ? 'Moving to Trash' : 'Moving'
    items.push({
      key: `op:${op.opId}`,
      label: op.current ? `${verb} · ${op.current}` : verb,
      detail: `${op.done}/${op.total}${op.state === 'error' ? ' · failed' : ''}`,
      value: op.total > 0 ? op.done / op.total : 0,
      tone: op.state === 'error' ? 'danger' : op.state === 'done' ? 'success' : 'amber',
      icon: op.op === 'delete' ? <Trash2 size={11} /> : <Move size={11} />,
    })
  }

  if (items.length === 0) return null

  return (
    <div className="flex h-7 w-full shrink-0 items-center gap-3 overflow-x-auto border-t border-hairline bg-panel/80 px-4 backdrop-blur">
      <Loader2 size={11} className="shrink-0 animate-spin text-amber" />
      {items.map((item) => {
        const content = (
          <>
            <span className={cn('shrink-0', item.tone === 'danger' ? 'text-danger' : 'text-ink-mut')}>{item.icon}</span>
            <span className="shrink-0 truncate text-[11.5px] text-ink">{item.label}</span>
            <span className="shrink-0 font-mono text-[10px] tabular text-ink-faint">{item.detail}</span>
            <Progress
              value={item.value}
              indeterminate={item.indeterminate}
              tone={item.tone}
              className="max-w-[120px] flex-1"
            />
          </>
        )
        return item.onClick ? (
          <button
            key={item.key}
            onClick={item.onClick}
            title={item.label}
            className="flex min-w-0 max-w-[320px] flex-1 items-center gap-2 text-left transition-opacity hover:opacity-80"
          >
            {content}
          </button>
        ) : (
          <span key={item.key} title={item.label} className="flex min-w-0 max-w-[320px] flex-1 items-center gap-2">
            {content}
          </span>
        )
      })}
    </div>
  )
}
