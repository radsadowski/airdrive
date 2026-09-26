import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  ArrowDownToLine,
  ClipboardPaste,
  Copy,
  FolderInput,
  Info,
  Move,
  Pencil,
  Scissors,
  Trash2,
} from 'lucide-react'
import type { FileInfo } from '@/lib/api/contract'
import { useAppStore } from '@/stores/app'
import { useUiStore } from '@/stores/ui'
import {
  copyOrCutSelection,
  downloadSelection,
  newFolder,
  openTransferPicker,
  pasteIntoCurrentDir,
  renameSelection,
  trashSelection,
} from '@/lib/actions'
import { cn } from '@/lib/utils'
import { formatBytes, formatDate } from '@/lib/format'
import { FileIcon, FileIconTile } from '@/components/file-icon'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'

const ROW_H = 34

function FileContextMenu({ file, children }: { file: FileInfo; children: React.ReactNode }) {
  const inspect = useUiStore((s) => s.inspect)
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuLabel>{file.name}</ContextMenuLabel>
        {file.kind === 'file' && (
          <ContextMenuItem onSelect={() => inspect(file)}>
            <Info /> Preview
          </ContextMenuItem>
        )}
        {file.kind === 'file' && (
          <ContextMenuItem onSelect={() => { inspect(file); downloadSelection() }}>
            <ArrowDownToLine /> Download
          </ContextMenuItem>
        )}
        <ContextMenuSeparator />
        <ContextMenuItem onSelect={() => openTransferPicker('copy')}>
          <Copy /> Copy to…
        </ContextMenuItem>
        <ContextMenuItem onSelect={() => openTransferPicker('move')}>
          <Move /> Move to…
        </ContextMenuItem>
        <ContextMenuItem onSelect={() => copyOrCutSelection('copy')}>
          <Copy /> Copy
        </ContextMenuItem>
        <ContextMenuItem onSelect={() => copyOrCutSelection('cut')}>
          <Scissors /> Cut
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem onSelect={() => renameSelection(file)}>
          <Pencil /> Rename
        </ContextMenuItem>
        <ContextMenuItem danger onSelect={() => void trashSelection()}>
          <Trash2 /> Move to Trash
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

function BrowserContextMenu({ children }: { children: React.ReactNode }) {
  const clipboard = useUiStore((s) => s.clipboard)
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem disabled={!clipboard} onSelect={() => void pasteIntoCurrentDir()}>
          <ClipboardPaste /> Paste {clipboard ? `(${clipboard.fileIds.length})` : ''}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem onSelect={() => newFolder()}>
          <FolderInput /> New folder
        </ContextMenuItem>
        <ContextMenuItem onSelect={() => useAppStore.getState().selectAll()}>
          <Info /> Select all
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

function RowThumb({ file }: { file: FileInfo }) {
  const [failed, setFailed] = useState(false)
  if (!file.thumbnailUrl || failed) return <FileIconTile file={file} size={44} />
  return (
    <img
      src={file.thumbnailUrl}
      alt=""
      loading="lazy"
      className="size-11 rounded-[6px] object-cover"
      draggable={false}
      onError={() => setFailed(true)}
    />
  )
}

export function FileBrowser() {
  const items = useAppStore((s) => s.items)
  const loading = useAppStore((s) => s.loading)
  const error = useAppStore((s) => s.error)
  const selection = useAppStore((s) => s.selection)
  const view = useAppStore((s) => s.view)
  const openDir = useAppStore((s) => s.openDir)
  const toggleSelect = useAppStore((s) => s.toggleSelect)
  const clearSelection = useAppStore((s) => s.clearSelection)
  const inspect = useUiStore((s) => s.inspect)
  const loadMore = useAppStore((s) => s.loadMore)
  const scrollRef = useRef<HTMLDivElement>(null)

  const selectedSet = useMemo(() => new Set(selection), [selection])

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_H,
    overscan: 14,
  })

  const onRowClick = useCallback(
    (e: React.MouseEvent, file: FileInfo) => {
      if (e.metaKey || e.ctrlKey) toggleSelect(file.id, { additive: true })
      else if (e.shiftKey) toggleSelect(file.id, { range: true })
      else if (file.kind === 'dir') void openDir({ id: file.id, name: file.name })
      else {
        toggleSelect(file.id)
        inspect(file)
      }
    },
    [toggleSelect, openDir, inspect],
  )

  // keyboard navigation
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
      const s = useAppStore.getState()
      if (s.items.length === 0) return
      const last = s.selection[s.selection.length - 1]
      const idx = s.items.findIndex((n) => n.id === last)

      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        const next = idx === -1 ? 0 : Math.min(s.items.length - 1, Math.max(0, idx + (e.key === 'ArrowDown' ? 1 : -1)))
        const node = s.items[next]
        if (e.metaKey || e.ctrlKey || e.shiftKey) toggleSelect(node.id, { additive: true })
        else {
          s.setSelection([node.id], node.id)
          inspect(node)
        }
      } else if (e.key === 'Enter') {
        const node = idx >= 0 ? s.items[idx] : null
        if (node?.kind === 'dir') void openDir({ id: node.id, name: node.name })
        else if (node) inspect(node)
      } else if (e.key === 'Escape') {
        clearSelection()
      } else if ((e.metaKey || e.ctrlKey) && e.key === 'a') {
        e.preventDefault()
        useAppStore.getState().selectAll()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [openDir, toggleSelect, inspect, clearSelection])

  if (error) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-center">
        <div className="font-mono text-[11px] uppercase tracking-widest text-danger">connection error</div>
        <div className="max-w-xs text-[13px] text-ink-mut">{error}</div>
      </div>
    )
  }

  if (loading && items.length === 0) {
    return (
      <div className="flex-1 overflow-hidden p-3">
        {Array.from({ length: 12 }).map((_, i) => (
          <div key={i} className="mb-1 h-[30px] rounded-[6px] bg-white/[0.035] animate-pulse" style={{ animationDelay: `${i * 40}ms` }} />
        ))}
      </div>
    )
  }

  if (items.length === 0 && !loading) {
    return (
      <BrowserContextMenu>
        <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
          <div className="flex size-14 items-center justify-center rounded-[12px] border border-hairline bg-white/[0.02]">
            <FolderInput size={22} className="text-ink-faint" />
          </div>
          <div>
            <div className="text-[13.5px] font-medium text-ink-mut">This folder is empty</div>
            <div className="mt-1 text-[12px] text-ink-faint">
              Drop files here to upload · right-click to paste · ⌘V
            </div>
          </div>
        </div>
      </BrowserContextMenu>
    )
  }

  /* ------------------------- list view ------------------------- */
  if (view === 'list') {
    return (
      <BrowserContextMenu>
        <div
          ref={scrollRef}
          className="relative flex-1 overflow-y-auto p-3 pt-2"
          onScroll={(e) => {
            const el = e.currentTarget
            if (el.scrollHeight - el.scrollTop - el.clientHeight < 600) void loadMore()
          }}
        >
          <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
            {virtualizer.getVirtualItems().map((v) => {
              const file = items[v.index]
              const selected = selectedSet.has(file.id)
              return (
                <div
                  key={file.id}
                  data-index={v.index}
                  className="absolute inset-x-0 top-0"
                  style={{ height: v.size, transform: `translateY(${v.start}px)` }}
                >
                  <div
                    style={{ animationDelay: `${Math.min(v.index, 24) * 12}ms` }}
                    className="animate-rise"
                  >
                    <FileContextMenu file={file}>
                      <div
                        onClick={(e) => onRowClick(e, file)}
                        onDoubleClick={() => file.kind === 'dir' && void openDir({ id: file.id, name: file.name })}
                        className={cn(
                          'group flex h-[30px] cursor-default items-center gap-3 rounded-[6px] border px-2.5 transition-colors',
                          selected
                            ? 'border-amber/30 bg-amber/[0.08]'
                            : 'border-transparent hover:border-hairline hover:bg-white/[0.03]',
                        )}
                      >
                        <FileIcon file={file} size={16} />
                        <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{file.name}</span>
                        <span className="w-[74px] shrink-0 text-right font-mono text-[11px] tabular text-ink-faint">
                          {file.kind === 'dir' ? '—' : formatBytes(file.size, 0)}
                        </span>
                        <span className="w-[92px] shrink-0 text-right font-mono text-[11px] tabular text-ink-faint">
                          {formatDate(file.modifiedAt)}
                        </span>
                      </div>
                    </FileContextMenu>
                  </div>
                </div>
              )
            })}
          </div>
          {loading && items.length > 0 && (
            <div className="flex items-center justify-center py-2 font-mono text-[10.5px] text-ink-faint">
              loading more…
            </div>
          )}
        </div>
      </BrowserContextMenu>
    )
  }

  /* ------------------------- grid view ------------------------- */

  return (
    <BrowserContextMenu>
      <div
        className="flex-1 overflow-y-auto p-4"
        onScroll={(e) => {
          const el = e.currentTarget
          if (el.scrollHeight - el.scrollTop - el.clientHeight < 800) void loadMore()
        }}
      >
        <div className="grid grid-cols-[repeat(auto-fill,minmax(104px,1fr))] gap-1.5">
          {items.map((file, i) => {
            const selected = selectedSet.has(file.id)
            return (
              <div key={file.id} style={{ animationDelay: `${Math.min(i, 24) * 12}ms` }} className="animate-rise">
                <FileContextMenu file={file}>
                  <div
                    onClick={(e) => onRowClick(e, file)}
                    onDoubleClick={() => file.kind === 'dir' && void openDir({ id: file.id, name: file.name })}
                    className={cn(
                      'flex cursor-default flex-col items-center gap-2 rounded-[8px] border px-2 py-3 transition-colors',
                      selected ? 'border-amber/40 bg-amber/[0.08]' : 'border-transparent hover:border-hairline hover:bg-white/[0.03]',
                    )}
                  >
                    <RowThumb file={file} />
                    <span className="line-clamp-2 w-full break-all text-center text-[11.5px] leading-tight text-ink">
                      {file.name}
                    </span>
                  </div>
                </FileContextMenu>
              </div>
            )
          })}
        </div>
      </div>
    </BrowserContextMenu>
  )
}
