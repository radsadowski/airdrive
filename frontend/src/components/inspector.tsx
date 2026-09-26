import { ArrowDownToLine, PanelRightClose } from 'lucide-react'
import { useAppStore } from '@/stores/app'
import { useUiStore } from '@/stores/ui'
import { downloadSelection } from '@/lib/actions'
import { formatBytes, formatDateTime } from '@/lib/format'
import { fileMeta } from '@/lib/api/mock/data'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Preview } from '@/components/preview'

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-[5px]">
      <span className="shrink-0 font-mono text-[9.5px] uppercase tracking-[0.12em] text-ink-faint">{label}</span>
      <span className="min-w-0 truncate text-right text-[12px] text-ink-mut" title={typeof children === 'string' ? children : undefined}>
        {children}
      </span>
    </div>
  )
}

export function Inspector() {
  const file = useUiStore((s) => s.inspectedFile)
  const setInspectorOpen = useUiStore((s) => s.setInspectorOpen)
  const locations = useAppStore((s) => s.locations)

  if (!file) return null
  const loc = locations.find((l) => l.id === file.locationId)
  const { category } = fileMeta(file.name)

  return (
    <aside className="flex h-full w-[300px] shrink-0 flex-col border-l border-hairline bg-panel/40">
      <div className="flex h-11 shrink-0 items-center gap-2 border-b border-hairline px-3">
        <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-ink-faint">inspector</span>
        <button
          onClick={() => setInspectorOpen(false)}
          className="ml-auto rounded p-1 text-ink-faint transition-colors hover:bg-white/10 hover:text-ink"
        >
          <PanelRightClose size={13} />
        </button>
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3">
        <div className="flex min-h-[180px] flex-col">
          <Preview file={file} />
        </div>

        <div>
          <div className="break-all text-[13px] font-medium leading-snug text-ink">{file.name}</div>
          <div className="mt-1 flex flex-wrap gap-1">
            <Badge tone="amber">{category}</Badge>
            {file.kind === 'file' && <Badge>{formatBytes(file.size)}</Badge>}
            {loc && <Badge tone="info">{loc.kind}</Badge>}
          </div>
        </div>

        <div className="rounded-[8px] border border-hairline bg-black/20 px-3 py-2">
          <Field label="location">{loc?.name ?? '—'}</Field>
          <Field label="modified">{formatDateTime(file.modifiedAt)}</Field>
          <Field label="mime">{file.mime}</Field>
          <Field label="md5">
            <span className="font-mono text-[10.5px]">{file.etag.slice(0, 16)}…</span>
          </Field>
        </div>

        {file.kind === 'file' && (
          <Button variant="secondary" size="sm" className="w-full" onClick={() => downloadSelection([file.id])}>
            <ArrowDownToLine /> Download to ~/Downloads
          </Button>
        )}
      </div>
    </aside>
  )
}
