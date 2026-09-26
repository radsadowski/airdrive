import { useCallback, useEffect, useState } from 'react'
import { ArrowLeft, Check, ChevronRight, Folder, Loader2 } from 'lucide-react'
import type { ConflictPolicy, FileInfo, Page } from '@/lib/api/contract'
import { api } from '@/lib/api'
import { useAppStore } from '@/stores/app'
import { useUiStore } from '@/stores/ui'
import { cn } from '@/lib/utils'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Kbd } from '@/components/ui/badge'

const POLICIES: { id: ConflictPolicy; label: string; desc: string }[] = [
  { id: 'smart', label: 'Smart (idempotent)', desc: 'skip if identical · keep both if different' },
  { id: 'overwrite', label: 'Overwrite', desc: 'destination always wins' },
  { id: 'skip', label: 'Skip existing', desc: 'never touch existing files' },
]

export function TransferPicker() {
  const picker = useUiStore((s) => s.transferPicker)
  const setPicker = useUiStore((s) => s.setTransferPicker)
  const locations = useAppStore((s) => s.locations)

  const [locId, setLocId] = useState<string | null>(null)
  const [path, setPath] = useState<{ id: string | null; name: string }[]>([])
  const [dir, setDir] = useState<Page<FileInfo> | null>(null)
  const [loading, setLoading] = useState(false)
  const [policy, setPolicy] = useState<ConflictPolicy>('smart')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (picker && !locId) {
      setLocId(useAppStore.getState().currentLocationId)
    }
  }, [picker, locId])

  const load = useCallback(async (locationId: string, dirId: string | null) => {
    setLoading(true)
    try {
      const page = await api.listDir(locationId, dirId, { sort: { field: 'name', dir: 'asc' } })
      setDir(page)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (picker && locId) {
      const cur = path[path.length - 1]
      void load(locId, cur?.id ?? null)
    }
  }, [picker, locId, path, load])

  if (!picker) return null

  const close = () => {
    setPicker(null)
    setLocId(null)
    setPath([])
    setDir(null)
  }

  const destDirId = path.length > 0 ? path[path.length - 1].id : null

  const confirm = async () => {
    if (!locId) return
    setBusy(true)
    try {
      await api.enqueueTransfer({
        action: picker.action,
        fileIds: picker.fileIds,
        destLocationId: locId,
        destDirId,
        conflictPolicy: policy,
      })
      close()
      useUiStore.getState().pushToast({
        title: `${picker.action === 'move' ? 'Move' : 'Copy'} queued`,
        description: 'Watch progress in the transfers bar',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && close()}>
      <DialogContent width="max-w-[520px]">
        <div className="px-5 pt-5">
          <div className="text-[15px] font-semibold tracking-tight text-ink">
            {picker.action === 'move' ? 'Move' : 'Copy'} {picker.fileIds.length} item{picker.fileIds.length !== 1 ? 's' : ''} to…
          </div>
          <div className="mt-0.5 text-[12px] text-ink-faint">
            {picker.action === 'move' ? 'Source is trashed after verified copy' : 'Files are streamed + MD5-verified'}
          </div>
        </div>

        <div className="px-5 py-3">
          {/* location select */}
          <div className="mb-2 flex flex-wrap gap-1">
            {locations.map((l) => (
              <button
                key={l.id}
                onClick={() => {
                  setLocId(l.id)
                  setPath([])
                }}
                className={cn(
                  'flex h-[26px] items-center gap-1.5 rounded-[6px] border px-2 text-[11.5px] transition-colors',
                  locId === l.id ? 'border-amber/50 bg-amber/10 text-amber' : 'border-hairline text-ink-mut hover:text-ink',
                )}
              >
                <span className={cn('size-[5px] rounded-full', l.kind === 'localfs' ? 'bg-loc-local' : l.kind === 'gdrive' ? 'bg-loc-gdrive' : 'bg-loc-s3')} />
                {l.name}
              </button>
            ))}
          </div>

          {/* mini tree browser */}
          <div className="h-[240px] overflow-y-auto rounded-[8px] border border-hairline bg-black/25">
            {path.length > 0 && (
              <div className="sticky top-0 z-10 flex items-center gap-1 border-b border-hairline bg-panel/95 px-2 py-1.5 backdrop-blur">
                <button
                  className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11.5px] text-ink-mut hover:bg-white/10 hover:text-ink"
                  onClick={() => setPath(path.slice(0, -1))}
                >
                  <ArrowLeft size={11} /> back
                </button>
                <span className="ml-1 truncate font-mono text-[10.5px] text-ink-faint">
                  /{path.map((p) => p.name).join('/')}
                </span>
              </div>
            )}
            {loading ? (
              <div className="flex h-full items-center justify-center">
                <Loader2 size={16} className="animate-spin text-ink-faint" />
              </div>
            ) : (
              (dir?.items ?? [])
                .filter((f) => f.kind === 'dir')
                .map((f) => (
                  <button
                    key={f.id}
                    onDoubleClick={() => setPath([...path, { id: f.id, name: f.name }])}
                    onClick={() => setPath([...path, { id: f.id, name: f.name }])}
                    className="flex w-full items-center gap-2 px-3 py-[5px] text-left text-[12.5px] text-ink-mut transition-colors hover:bg-white/[0.04] hover:text-ink"
                  >
                    <Folder size={13.5} className="shrink-0 text-amber/70" />
                    <span className="min-w-0 flex-1 truncate">{f.name}</span>
                    <ChevronRight size={12} className="shrink-0 text-ink-faint" />
                  </button>
                ))
            )}
            {dir && dir.items.filter((f) => f.kind === 'dir').length === 0 && !loading && (
              <div className="px-3 py-2 text-[11.5px] text-ink-faint">no subfolders — drop here</div>
            )}
          </div>

          {/* conflict policy */}
          <div className="mt-2.5 flex items-center gap-1.5">
            <span className="font-mono text-[9.5px] uppercase tracking-widest text-ink-faint">on conflict</span>
            {POLICIES.map((p) => (
              <button
                key={p.id}
                title={p.desc}
                onClick={() => setPolicy(p.id)}
                className={cn(
                  'flex h-[24px] items-center rounded-[5px] border px-2 text-[11px] transition-colors',
                  policy === p.id ? 'border-amber/50 bg-amber/10 text-amber' : 'border-hairline text-ink-faint hover:text-ink-mut',
                )}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>

        <div className="flex items-center justify-between border-t border-hairline px-5 py-3">
          <span className="font-mono text-[10px] text-ink-faint">
            dest: {locations.find((l) => l.id === locId)?.name ?? '—'}
            {path.length > 0 ? `/${path.map((p) => p.name).join('/')}` : ''}
          </span>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={close}>Cancel</Button>
            <Button variant="primary" disabled={!locId || busy} onClick={() => void confirm()}>
              {busy ? <Loader2 size={13} className="animate-spin" /> : <Check size={14} />}
              {picker.action === 'move' ? 'Move here' : 'Copy here'}
              <Kbd className="ml-1 border-black/30 bg-black/40">⌘↵</Kbd>
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
