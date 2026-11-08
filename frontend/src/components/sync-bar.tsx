import { ArrowLeftRight, Loader2 } from 'lucide-react'
import { useSyncStore } from '@/stores/sync'
import { useUiStore } from '@/stores/ui'
import { Progress } from '@/components/ui/progress'
import { formatBytes, formatSpeed } from '@/lib/format'
import { useEffect, useRef, useState } from 'react'

/**
 * SyncActivityBar: the global sync status strip across the bottom of the
 * window. Shows every running pass with live progress fed by the engine's
 * sync:progress events; hides itself entirely when nothing is syncing.
 */
export function SyncActivityBar() {
  const activeRuns = useSyncStore((s) => s.activeRuns)
  const setSyncOpen = useUiStore((s) => s.setSyncOpen)
  const runs = Object.values(activeRuns)

  const [peak, setPeak] = useState<Record<string, { bytes: number; at: number }>>({})
  const [speeds, setSpeeds] = useState<Record<string, number>>({})
  const lastSeen = useRef<Record<string, { bytes: number; at: number }>>({})

  /* derive per-run speed from cumulative bytes across ticks */
  useEffect(() => {
    const next: Record<string, number> = {}
    for (const r of runs) {
      const prev = lastSeen.current[r.pairId]
      const now = Date.now()
      if (prev && now > prev.at && r.bytesDone >= prev.bytes) {
        next[r.pairId] = (r.bytesDone - prev.bytes) / ((now - prev.at) / 1000)
      } else if (peak[r.pairId] && now - peak[r.pairId].at < 2500) {
        next[r.pairId] = peak[r.pairId].bytes
      } else {
        next[r.pairId] = 0
      }
      lastSeen.current[r.pairId] = { bytes: r.bytesDone, at: now }
      if (next[r.pairId] > 0) {
        setPeak((p) => ({ ...p, [r.pairId]: { bytes: next[r.pairId], at: now } }))
      }
    }
    setSpeeds(next)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs])

  if (runs.length === 0) return null

  return (
    <button
      onClick={() => setSyncOpen(true)}
      title="Open sync policies"
      className="flex h-7 w-full shrink-0 items-center gap-3 border-t border-hairline bg-panel/80 px-4 backdrop-blur transition-colors hover:bg-panel"
    >
      <span className="flex items-center gap-1.5 text-amber">
        <Loader2 size={11} className="animate-spin" />
        <ArrowLeftRight size={11} />
      </span>
      <span className="truncate text-[11.5px] text-ink">
        {runs.length === 1 ? (runs[0].name || 'Syncing…') : `${runs.length} syncs running`}
      </span>
      {runs.map((r) => (
        <span key={r.pairId} className="flex min-w-0 flex-1 items-center gap-2">
          <span className="shrink-0 font-mono text-[10px] tabular text-ink-faint">
            {r.filesTotal > 0 ? `${r.filesDone}/${r.filesTotal}` : '…'} · {formatBytes(r.bytesDone)}
            {(speeds[r.pairId] ?? 0) > 0 ? ` · ${formatSpeed(speeds[r.pairId])}` : ''}
          </span>
          <Progress
            value={r.bytesTotal > 0 ? r.bytesDone / r.bytesTotal : 0}
            className="h-[3px] max-w-[180px] flex-1"
          />
        </span>
      ))}
    </button>
  )
}
