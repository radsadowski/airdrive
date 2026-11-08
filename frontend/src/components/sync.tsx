import { useEffect, useState } from 'react'
import {
  ArrowRight,
  ArrowLeft,
  ArrowLeftRight,
  FolderOpen,
  Loader2,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Trash2,
  X,
} from 'lucide-react'
import type { FileInfo, Location, SyncDirection, SyncPair } from '@/lib/api/contract'
import {
  SYNC_ACT_CREATE,
  SYNC_ACT_DELETE,
  SYNC_ACT_MODIFY,
} from '@/lib/api/contract'
import { api } from '@/lib/api'
import { useAppStore } from '@/stores/app'
import { useSyncStore, parseCounts } from '@/stores/sync'
import { useUiStore, toastError } from '@/stores/ui'
import { cn } from '@/lib/utils'
import { relativeTime } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogBody,
  DialogFooter,
} from '@/components/ui/dialog'

/* ------------------------------------------------------------------ */
/* Folder picker: lazy drill-down into one location's directories      */
/* ------------------------------------------------------------------ */

function FolderPicker({
  location,
  onChange,
}: {
  location: Location
  onChange: (dirId: string | null, label: string) => void
}) {
  const [stack, setStack] = useState<FileInfo[]>([])
  const [dirs, setDirs] = useState<FileInfo[]>([])
  const [loading, setLoading] = useState(false)

  const currentDir = stack[stack.length - 1]

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    void api
      .listDir(location.id, currentDir?.id ?? null, { limit: 200 })
      .then((page) => {
        if (!cancelled) setDirs(page.items.filter((i) => i.kind === 'dir'))
      })
      .catch(() => void 0)
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.id, currentDir?.id])

  return (
    <div className="rounded-[8px] border border-hairline bg-base/60 p-2">
      <div className="mb-2 flex items-center gap-1 text-[11px] font-mono text-ink-faint">
        <button
          onClick={() => setStack([])}
          className={cn('rounded px-1.5 py-0.5 transition-colors hover:bg-white/[0.06] hover:text-ink', stack.length === 0 && 'text-amber')}
        >
          root
        </button>
        {stack.map((d, i) => (
          <span key={d.id} className="flex items-center gap-1">
            <span className="text-ink-faint/60">/</span>
            <button
              onClick={() => setStack(stack.slice(0, i + 1))}
              className={cn('rounded px-1.5 py-0.5 transition-colors hover:bg-white/[0.06] hover:text-ink', i === stack.length - 1 && 'text-amber')}
            >
              {d.name}
            </button>
          </span>
        ))}
        <button
          onClick={() => onChange(currentDir?.id ?? null, currentDir?.name ?? 'root')}
          className="ml-auto rounded bg-amber/15 px-2 py-0.5 text-[10.5px] font-medium text-amber transition-colors hover:bg-amber/25"
        >
          use {currentDir ? `"${currentDir.name}"` : 'whole storage'}
        </button>
      </div>
      <div className="max-h-[132px] overflow-y-auto">
        {loading && <div className="px-1.5 py-2 text-[11px] text-ink-faint">Loading…</div>}
        {!loading && dirs.length === 0 && (
          <div className="px-1.5 py-2 text-[11px] text-ink-faint">No folders here</div>
        )}
        {dirs.map((d) => (
          <button
            key={d.id}
            onClick={() => setStack((s) => [...s, d])}
            className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-left text-[12px] text-ink-mut transition-colors hover:bg-white/[0.05] hover:text-ink"
          >
            <FolderOpen size={12} className="text-ink-faint" />
            <span className="truncate">{d.name}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* Policy dialog (create + edit)                                       */
/* ------------------------------------------------------------------ */

const DIRECTION_CARDS: {
  dir: SyncDirection
  icon: typeof ArrowRight
  title: string
  body: string
}[] = [
  {
    dir: 'a_to_b',
    icon: ArrowRight,
    title: 'Push',
    body: 'Files only ever travel to the right-hand storage. The left one stays the source of truth.',
  },
  {
    dir: 'b_to_a',
    icon: ArrowLeft,
    title: 'Pull',
    body: 'Files only ever travel to the left-hand storage. The right one stays untouched by you.',
  },
  {
    dir: 'both',
    icon: ArrowLeftRight,
    title: 'Keep mirrored',
    body: 'Both storages copy to each other. When both sides changed the same file, the newest wins.',
  },
]

function SyncPolicyDialog({
  initial,
  onClose,
}: {
  initial: SyncPair | null
  onClose: (saved: boolean) => void
}) {
  const locations = useAppStore((s) => s.locations)
  const save = useSyncStore((s) => s.save)
  const [busy, setBusy] = useState(false)

  const [locationA, setLocationA] = useState(initial?.locationA ?? locations[0]?.id ?? '')
  const [locationB, setLocationB] = useState(initial?.locationB ?? locations[1]?.id ?? '')
  const [pathA, setPathA] = useState<string | null>(initial?.pathA || null)
  const [labelA, setLabelA] = useState(initial?.pathA ? initial.pathA : 'whole storage')
  const [pathB, setPathB] = useState<string | null>(initial?.pathB || null)
  const [labelB, setLabelB] = useState(initial?.pathB ? initial.pathB : 'whole storage')
  const [direction, setDirection] = useState<SyncDirection>(initial?.direction ?? 'a_to_b')
  const [actions, setActions] = useState(initial?.actions ?? SYNC_ACT_CREATE | SYNC_ACT_MODIFY)
  const [backfill, setBackfill] = useState(true)
  const [error, setError] = useState('')
  const isNew = initial === null

  const toggleAction = (bit: number) => setActions((a) => (a & bit ? a & ~bit : a | bit))

  const handleSave = async () => {
    setError('')
    if (!locationA || !locationB) return
    if (locationA === locationB) {
      setError('Pick two different storages: a storage cannot sync with itself.')
      return
    }
    setBusy(true)
    const payload: SyncPair = {
      id: initial?.id ?? '',
      name: initial?.name ?? '',
      locationA,
      pathA: pathA ?? '',
      locationB,
      pathB: pathB ?? '',
      direction,
      actions,
      paused: initial?.paused ?? false,
      backfill: isNew ? backfill : false,
    }
    try {
      await save(payload, isNew)
      onClose(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  const locA = locations.find((l) => l.id === locationA)
  const locB = locations.find((l) => l.id === locationB)

  return (
    <Dialog open onOpenChange={(open) => !open && onClose(false)}>
      <DialogContent width="max-w-xl">
        <DialogHeader>
          <DialogTitle>{isNew ? 'New sync policy' : 'Edit sync policy'}</DialogTitle>
        </DialogHeader>
        <DialogBody className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            {(['A', 'B'] as const).map((side) => {
              const loc = side === 'A' ? locA : locB
              const value = side === 'A' ? locationA : locationB
              const pathLabel = side === 'A' ? labelA : labelB
              const setLoc = side === 'A' ? setLocationA : setLocationB
              const setPath = side === 'A' ? setPathA : setPathB
              const setLabel = side === 'A' ? setLabelA : setLabelB
              return (
                <div key={side} className="space-y-2">
                  <label className="block font-mono text-[9.5px] uppercase tracking-[0.14em] text-ink-faint">
                    {side === 'A' ? 'Left storage' : 'Right storage'}
                  </label>
                  <select
                    value={value}
                    onChange={(e) => {
                      setLoc(e.target.value)
                      setPath(null)
                      setLabel('whole storage')
                    }}
                    className="w-full rounded-[7px] border border-hairline bg-base px-2 py-1.5 text-[12.5px] text-ink outline-none focus:border-amber/60"
                  >
                    {locations.map((l) => (
                      <option key={l.id} value={l.id}>
                        {l.name}
                      </option>
                    ))}
                  </select>
                  <div className="text-[10.5px] text-ink-faint">
                    scope: <span className="text-ink-mut">{pathLabel}</span>
                  </div>
                  {loc && (
                    <FolderPicker
                      location={loc}
                      onChange={(dirId, label) => {
                        setPath(dirId)
                        setLabel(dirId ? label : 'whole storage')
                      }}
                    />
                  )}
                </div>
              )
            })}
          </div>

          <div className="space-y-2">
            <label className="block font-mono text-[9.5px] uppercase tracking-[0.14em] text-ink-faint">
              How should they sync?
            </label>
            <div className="grid grid-cols-3 gap-2">
              {DIRECTION_CARDS.map(({ dir, icon: Icon, title, body }) => (
                <button
                  key={dir}
                  onClick={() => setDirection(dir)}
                  className={cn(
                    'rounded-[9px] border px-3 py-2.5 text-left transition-colors',
                    direction === dir
                      ? 'border-amber/60 bg-amber/10 text-ink'
                      : 'border-hairline text-ink-mut hover:border-hairline-strong hover:text-ink',
                  )}
                >
                  <Icon size={14} className={direction === dir ? 'text-amber' : 'text-ink-faint'} />
                  <div className="mt-1.5 text-[12px] font-semibold">{title}</div>
                  <div className="mt-1 text-[10.5px] leading-snug text-ink-faint">{body}</div>
                </button>
              ))}
            </div>
          </div>

          <div className="space-y-2">
            <label className="block font-mono text-[9.5px] uppercase tracking-[0.14em] text-ink-faint">
              What counts as a change?
            </label>
            <div className="space-y-1.5">
              {[
                { bit: SYNC_ACT_CREATE, label: 'New files and folders', hint: 'Anything that appears gets copied over.' },
                { bit: SYNC_ACT_MODIFY, label: 'Changed content', hint: 'Edited files overwrite the other side.' },
                { bit: SYNC_ACT_DELETE, label: 'Deletions', hint: 'Removing from one side removes from the other, moving files to the trash folder each storage provides.' },
              ].map(({ bit, label, hint }) => (
                <label
                  key={bit}
                  className={cn(
                    'flex cursor-pointer items-start gap-2.5 rounded-[8px] border px-3 py-2 transition-colors',
                    actions & bit ? 'border-amber/50 bg-amber/5' : 'border-hairline hover:border-hairline-strong',
                  )}
                >
                  <input
                    type="checkbox"
                    checked={(actions & bit) !== 0}
                    onChange={() => toggleAction(bit)}
                    className="mt-0.5 accent-amber"
                  />
                  <span>
                    <span className="block text-[12px] font-medium text-ink">{label}</span>
                    <span className="block text-[10.5px] leading-snug text-ink-faint">
                      {hint}
                      {bit === SYNC_ACT_DELETE && actions & bit && (
                        <span className="mt-0.5 block text-amber/90">
                          Watch out: with this on, tidying one storage tidies both.
                        </span>
                      )}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </div>

          {isNew && (
            <label
              className={cn(
                'flex cursor-pointer items-start gap-2.5 rounded-[8px] border px-3 py-2 transition-colors',
                backfill ? 'border-amber/50 bg-amber/5' : 'border-hairline hover:border-hairline-strong',
              )}
            >
              <input
                type="checkbox"
                checked={backfill}
                onChange={() => setBackfill(!backfill)}
                className="mt-0.5 accent-amber"
              />
              <span>
                <span className="block text-[12px] font-medium text-ink">Copy existing files right away</span>
                <span className="block text-[10.5px] leading-snug text-ink-faint">
                  Starts by matching both sides once. Turn off if this is a brand-new pair of folders.
                </span>
              </span>
            </label>
          )}

          {error && <p className="text-[12px] text-danger">{error}</p>}
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(false)}>Cancel</Button>
          <Button variant="primary" disabled={busy} onClick={() => void handleSave()}>
            {busy && <Loader2 size={13} className="animate-spin" />}
            {isNew ? 'Create pair' : 'Save changes'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/* ------------------------------------------------------------------ */
/* Pair row                                                            */
/* ------------------------------------------------------------------ */

function PairCard({ pair, onEdit }: { pair: SyncPair; onEdit: () => void }) {
  const remove = useSyncStore((s) => s.remove)
  const setPaused = useSyncStore((s) => s.setPaused)
  const runNow = useSyncStore((s) => s.runNow)
  const [removing, setRemoving] = useState(false)
  const [running, setRunning] = useState(false)
  const locations = useAppStore((s) => s.locations)

  const counts = parseCounts(pair.lastCounts)
  const nameA = locations.find((l) => l.id === pair.locationA)?.name ?? '?'
  const nameB = locations.find((l) => l.id === pair.locationB)?.name ?? '?'
  const Glyph = pair.direction === 'both' ? ArrowLeftRight : pair.direction === 'b_to_a' ? ArrowLeft : ArrowRight

  const badges: string[] = []
  if (pair.actions & SYNC_ACT_CREATE) badges.push('additions')
  if (pair.actions & SYNC_ACT_MODIFY) badges.push('updates')
  if (pair.actions & SYNC_ACT_DELETE) badges.push('deletions')

  const handleRun = async () => {
    setRunning(true)
    try {
      await runNow(pair.id)
      setTimeout(() => setRunning(false), 1500)
    } catch (e) {
      setRunning(false)
      toastError(e)
    }
  }

  return (
    <div
      className={cn(
        'rounded-[10px] border border-hairline bg-panel/70 p-3 transition-opacity',
        pair.paused && 'opacity-60',
      )}
    >
      <div className="flex items-center gap-2">
        <Glyph size={15} className="shrink-0 text-amber" />
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-ink">
          {nameA}
          <span className="text-ink-faint"> {pair.direction === 'both' ? 'mirrors' : 'syncs with'} </span>
          {nameB}
        </span>
        <span className="flex shrink-0 items-center gap-0.5">
          <button
            title={running ? 'Syncing…' : 'Sync now'}
            onClick={() => void handleRun()}
            className="rounded p-1.5 text-ink-faint transition-colors hover:bg-white/[0.07] hover:text-amber"
          >
            {running ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
          </button>
          <button
            title={pair.paused ? 'Resume syncing' : 'Pause syncing'}
            onClick={() => void setPaused(pair.id, !pair.paused)}
            className="rounded p-1.5 text-ink-faint transition-colors hover:bg-white/[0.07] hover:text-ink"
          >
            {pair.paused ? <Play size={14} /> : <Pause size={14} />}
          </button>
          <button
            title="Edit policy"
            onClick={onEdit}
            className="rounded p-1.5 text-ink-faint transition-colors hover:bg-white/[0.07] hover:text-ink"
          >
            <Pencil size={14} />
          </button>
          <button
            title="Delete policy"
            onClick={async () => {
              setRemoving(true)
              try {
                await remove(pair.id)
              } catch (e) {
                toastError(e)
                setRemoving(false)
              }
            }}
            className="rounded p-1.5 text-ink-faint transition-colors hover:bg-white/[0.07] hover:text-danger"
          >
            <Trash2 size={14} />
          </button>
        </span>
      </div>
      <div className="mt-1.5 truncate font-mono text-[10.5px] text-ink-faint">
        {pair.pathA || 'root'} {pair.direction === 'both' ? '↔' : pair.direction === 'b_to_a' ? '←' : '→'} {pair.pathB || 'root'}
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        {badges.map((b) => (
          <span key={b} className="rounded-full bg-white/[0.06] px-2 py-0.5 text-[10px] font-medium text-ink-mut">
            {b}
          </span>
        ))}
        {pair.paused && (
          <span className="rounded-full bg-amber/15 px-2 py-0.5 text-[10px] font-medium text-amber">paused</span>
        )}
        <span className="ml-auto text-[10.5px] text-ink-faint">
          {pair.lastError
            ? pair.lastError
            : pair.lastRunAt
              ? counts && counts.copied + counts.deleted > 0
                ? `last run ${relativeTime(pair.lastRunAt)}: ${counts.copied} copied${counts.deleted > 0 ? ` · ${counts.deleted} deleted` : ''}${counts.failed > 0 ? ` · ${counts.failed} failed` : ''}`
                : `last run ${relativeTime(pair.lastRunAt)}: everything matched`
              : 'never run yet'}
        </span>
        {removing && <Loader2 size={12} className="animate-spin text-ink-faint" />}
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* The panel                                                           */
/* ------------------------------------------------------------------ */

export function SyncPanel() {
  const syncOpen = useUiStore((s) => s.syncOpen)
  const setSyncOpen = useUiStore((s) => s.setSyncOpen)
  const pairs = useSyncStore((s) => s.pairs)
  const load = useSyncStore((s) => s.load)
  const reloadLocations = useAppStore((s) => s.reloadLocations)
  const [dialog, setDialog] = useState<{ open: boolean; pair: SyncPair | null }>({ open: false, pair: null })

  useEffect(() => {
    if (!syncOpen) return
    void load().catch(toastError)
    void reloadLocations()
  }, [syncOpen, load, reloadLocations])

  if (!syncOpen) return null

  return (
    <Dialog open onOpenChange={(open) => !open && setSyncOpen(false)}>
      <DialogContent width="max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            Sync policies
            <span className="ml-3 text-[11px] font-normal text-ink-faint">
              {pairs.length} {pairs.length === 1 ? 'pair' : 'pairs'}
            </span>
          </DialogTitle>
        </DialogHeader>
        <DialogBody className="max-h-[60vh] space-y-2 overflow-y-auto">
          {pairs.length === 0 && (
            <div className="rounded-[10px] border border-dashed border-hairline px-4 py-8 text-center">
              <ArrowLeftRight size={22} className="mx-auto text-ink-faint" />
              <p className="mt-3 text-[13px] text-ink-mut">No sync policies yet.</p>
              <p className="mt-1 text-[11.5px] leading-relaxed text-ink-faint">
                Keep a local folder in step with Drive, or mirror a Drive folder into your R2 bucket.
                <br />
                New files follow on their own; everything lands in the Transfers drawer.
              </p>
            </div>
          )}
          {pairs.map((p) => (
            <PairCard key={p.id} pair={p} onEdit={() => setDialog({ open: true, pair: p })} />
          ))}
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setSyncOpen(false)}>
            <X size={13} /> Close
          </Button>
          <Button variant="primary" onClick={() => setDialog({ open: true, pair: null })}>
            <Plus size={13} /> New sync policy
          </Button>
        </DialogFooter>
      </DialogContent>

      {dialog.open && (
        <SyncPolicyDialog
          initial={dialog.pair}
          onClose={() => setDialog({ open: false, pair: null })}
        />
      )}
    </Dialog>
  )
}
