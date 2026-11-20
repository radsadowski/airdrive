import { useEffect, useRef } from 'react'
import { Terminal, Trash2, X } from 'lucide-react'
import { useLogsStore } from '@/stores/logs'
import { useUiStore } from '@/stores/ui'
import { cn } from '@/lib/utils'

const LEVEL_STYLE: Record<string, string> = {
  DEBUG: 'text-ink-faint',
  INFO: 'text-ink-mut',
  WARN: 'text-amber',
  ERROR: 'text-danger',
}

/** Debug log stream: file operations, transfers, crawls and HTTP exchanges. */
export function LogPanel() {
  const open = useUiStore((s) => s.logsOpen)
  const setOpen = useUiStore((s) => s.setLogsOpen)
  const lines = useLogsStore((s) => s.lines)
  const clear = useLogsStore((s) => s.clear)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines, open])

  if (!open) return null

  return (
    <div className="flex h-[220px] w-full shrink-0 flex-col border-t border-hairline bg-base/95 backdrop-blur">
      <div className="flex h-8 shrink-0 items-center gap-2 border-b border-hairline px-4">
        <Terminal size={12} className="text-amber" />
        <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-ink-faint">debug log</span>
        <span className="font-mono text-[10px] tabular text-ink-faint">{lines.length} lines</span>
        <button
          onClick={clear}
          title="Clear log"
          className="ml-auto rounded p-1 text-ink-faint transition-colors hover:bg-white/10 hover:text-ink"
        >
          <Trash2 size={12} />
        </button>
        <button
          onClick={() => setOpen(false)}
          title="Close log"
          className="rounded p-1 text-ink-faint transition-colors hover:bg-white/10 hover:text-ink"
        >
          <X size={13} />
        </button>
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-4 py-2 font-mono text-[11px] leading-relaxed">
        {lines.length === 0 && (
          <div className="text-ink-faint">
            No entries yet. File operations, transfers, crawls and HTTP calls appear here while debug logging is on.
          </div>
        )}
        {lines.map((line, i) => (
          <div key={`${line.time}-${i}`} className="flex gap-2">
            <span className="shrink-0 text-ink-faint">{line.time.slice(11, 23)}</span>
            <span className={cn('w-[44px] shrink-0', LEVEL_STYLE[line.level] ?? 'text-ink-mut')}>{line.level}</span>
            <span className="shrink-0 text-ink">{line.msg}</span>
            {line.attrs && Object.keys(line.attrs).length > 0 && (
              <span className="truncate text-ink-faint">
                {Object.entries(line.attrs)
                  .map(([k, v]) => `${k}=${v}`)
                  .join(' ')}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
