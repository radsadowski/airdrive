import { useEffect, useRef, useState } from 'react'
import {
  ArrowDown,
  ArrowUp,
  Calendar,
  FileImage,
  FileVideo,
  FileAudio,
  FileText,
  FileCode2,
  Archive,
  Filter,
  HardDrive,
  Search,
  X,
} from 'lucide-react'
import type { FileCategory, FileInfo } from '@/lib/api/contract'
import { api } from '@/lib/api'
import { useAppStore } from '@/stores/app'
import { useUiStore } from '@/stores/ui'
import { cn } from '@/lib/utils'
import { formatBytes, formatDate } from '@/lib/format'
import { FileIcon } from '@/components/file-icon'
import { Kbd } from '@/components/ui/badge'

const CATEGORIES: { id: FileCategory; label: string; icon: typeof FileImage }[] = [
  { id: 'image', label: 'Images', icon: FileImage },
  { id: 'video', label: 'Video', icon: FileVideo },
  { id: 'audio', label: 'Audio', icon: FileAudio },
  { id: 'document', label: 'Docs', icon: FileText },
  { id: 'pdf', label: 'PDF', icon: FileText },
  { id: 'code', label: 'Code', icon: FileCode2 },
  { id: 'archive', label: 'Archives', icon: Archive },
]

type SizeRange = 'any' | 'kb' | 'mb' | 'gb'

const SIZE_RANGES: Record<SizeRange, [number | undefined, number | undefined]> = {
  any: [undefined, undefined],
  kb: [1_000, 1_000_000],
  mb: [1_000_000, 1_000_000_000],
  gb: [1_000_000_000, undefined],
}

export function SearchOverlay() {
  const open = useUiStore((s) => s.searchOpen)
  const setOpen = useUiStore((s) => s.setSearchOpen)
  const locations = useAppStore((s) => s.locations)
  const revealFile = useAppStore((s) => s.revealFile)

  const [query, setQuery] = useState('')
  const [cats, setCats] = useState<Set<FileCategory>>(new Set())
  const [size, setSize] = useState<SizeRange>('any')
  const [scope, setScope] = useState<string[]>([]) // empty = all locations
  const [results, setResults] = useState<FileInfo[]>([])
  const [searching, setSearching] = useState(false)
  const [cursor, setCursor] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 20)
    } else {
      setQuery('')
      setCursor(0)
    }
  }, [open])

  useEffect(() => {
    if (!open || query.trim().length === 0) {
      setResults([])
      return
    }
    setSearching(true)
    const t = setTimeout(async () => {
      const [sizeMin, sizeMax] = SIZE_RANGES[size]
      try {
        const page = await api.search({
          text: query.trim(),
          categories: cats.size > 0 ? [...cats] : undefined,
          locationIds: scope.length > 0 ? scope : undefined,
          sizeMin,
          sizeMax,
          limit: 120,
        })
        setResults(page.items)
        setCursor(0)
      } catch (e) {
        console.error('[search] search failed', e)
      } finally {
        setSearching(false)
      }
    }, 180)
    return () => clearTimeout(t)
  }, [query, cats, size, scope, open])

  const locName = (id: string) => locations.find((l) => l.id === id)?.name ?? ''

  const reveal = async (file: FileInfo) => {
    setOpen(false)
    await revealFile(file)
  }

  if (!open) return null

  const flat = results

  return (
    <div className="fixed inset-0 z-[65] flex items-start justify-center pt-[12vh]" onClick={() => setOpen(false)}>
      <div className="absolute inset-0 bg-black/55 backdrop-blur-[3px]" />
      <div
        onClick={(e) => e.stopPropagation()}
        className="relative w-[min(640px,calc(100vw-32px))] animate-pop-in overflow-hidden rounded-[12px] border border-hairline-strong bg-overlay shadow-[0_32px_90px_-12px_rgba(0,0,0,0.9)]"
      >
        {/* query */}
        <div className="flex items-center gap-2.5 border-b border-hairline px-4 py-3">
          <Search size={15} className="shrink-0 text-amber" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault()
                setCursor((c) => Math.min(flat.length - 1, c + 1))
              } else if (e.key === 'ArrowUp') {
                e.preventDefault()
                setCursor((c) => Math.max(0, c - 1))
              } else if (e.key === 'Enter' && flat[cursor]) {
                void reveal(flat[cursor])
              } else if (e.key === 'Escape') {
                setOpen(false)
              }
            }}
            placeholder="Search every storage…"
            spellCheck={false}
            className="flex-1 bg-transparent text-[14.5px] text-ink placeholder:text-ink-faint"
          />
          {searching && <span className="size-3 animate-spin rounded-full border border-amber border-t-transparent" />}
          <Kbd>esc</Kbd>
        </div>

        {/* filters */}
        <div className="flex flex-wrap items-center gap-1.5 border-b border-hairline px-3 py-2">
          <span className="mr-1 flex items-center gap-1 font-mono text-[9.5px] uppercase tracking-widest text-ink-faint">
            <Filter size={10} /> type
          </span>
          {CATEGORIES.map((c) => {
            const on = cats.has(c.id)
            return (
              <button
                key={c.id}
                onClick={() => {
                  const next = new Set(cats)
                  if (on) next.delete(c.id)
                  else next.add(c.id)
                  setCats(next)
                }}
                className={cn(
                  'flex h-[23px] items-center gap-1 rounded-[5px] border px-1.5 text-[11px] transition-colors',
                  on ? 'border-amber/50 bg-amber/10 text-amber' : 'border-hairline text-ink-faint hover:text-ink-mut',
                )}
              >
                <c.icon size={10.5} /> {c.label}
              </button>
            )
          })}
          <span className="mx-1 h-3.5 w-px bg-hairline" />
          <span className="mr-1 flex items-center gap-1 font-mono text-[9.5px] uppercase tracking-widest text-ink-faint">
            <Calendar size={10} /> size
          </span>
          {(['any', 'kb', 'mb', 'gb'] as SizeRange[]).map((s) => (
            <button
              key={s}
              onClick={() => setSize(s)}
              className={cn(
                'flex h-[23px] items-center rounded-[5px] border px-1.5 font-mono text-[10.5px] uppercase transition-colors',
                size === s ? 'border-amber/50 bg-amber/10 text-amber' : 'border-hairline text-ink-faint hover:text-ink-mut',
              )}
            >
              {s === 'any' ? 'any' : `≥ ${s}`}
            </button>
          ))}
          {locations.length > 1 && (
            <>
              <span className="mx-1 h-3.5 w-px bg-hairline" />
              <button
                onClick={() => setScope(scope.length === locations.length ? [] : locations.map((l) => l.id))}
                className={cn(
                  'flex h-[23px] items-center gap-1 rounded-[5px] border px-1.5 text-[11px] transition-colors',
                  scope.length === 0 ? 'border-hairline text-ink-faint hover:text-ink-mut' : 'border-amber/50 bg-amber/10 text-amber',
                )}
              >
                <HardDrive size={10.5} /> {scope.length === 0 ? 'all storages' : `${scope.length} scoped`}
              </button>
            </>
          )}
          {(cats.size > 0 || size !== 'any' || scope.length > 0) && (
            <button
              onClick={() => {
                setCats(new Set())
                setSize('any')
                setScope([])
              }}
              className="ml-auto flex h-[23px] items-center gap-1 rounded-[5px] px-1.5 text-[11px] text-ink-faint hover:text-danger"
            >
              <X size={10} /> clear
            </button>
          )}
        </div>

        {/* results */}
        <div ref={listRef} className="max-h-[46vh] overflow-y-auto p-1.5">
          {query.trim().length === 0 ? (
            <div className="px-3 py-8 text-center text-[12.5px] text-ink-faint">
              Type to search across <span className="text-ink-mut">{locations.length} storages</span> —
              filenames, filters by type & size.
            </div>
          ) : results.length === 0 && !searching ? (
            <div className="px-3 py-8 text-center text-[12.5px] text-ink-faint">
              No files match <span className="text-ink-mut">“{query}”</span>
            </div>
          ) : (
            results.map((file, i) => (
              <button
                key={file.id}
                onMouseEnter={() => setCursor(i)}
                onClick={() => void reveal(file)}
                className={cn(
                  'flex w-full items-center gap-3 rounded-[7px] px-2.5 py-2 text-left transition-colors',
                  i === cursor ? 'bg-amber/10 ring-1 ring-amber/25' : 'hover:bg-white/[0.03]',
                )}
              >
                <FileIcon file={file} size={17} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] text-ink">{file.name}</span>
                  <span className="block truncate font-mono text-[10.5px] text-ink-faint">{locName(file.locationId)}</span>
                </span>
                <span className="shrink-0 font-mono text-[10.5px] tabular text-ink-faint">
                  {file.kind === 'dir' ? 'folder' : formatBytes(file.size, 0)}
                </span>
                <span className="w-[80px] shrink-0 text-right font-mono text-[10.5px] tabular text-ink-faint">
                  {formatDate(file.modifiedAt)}
                </span>
              </button>
            ))
          )}
        </div>

        <div className="flex items-center gap-3 border-t border-hairline px-3.5 py-1.5 font-mono text-[9.5px] text-ink-faint">
          <span className="flex items-center gap-1"><Kbd>↵</Kbd> reveal</span>
          <span className="flex items-center gap-1"><Kbd><ArrowUp size={8} /></Kbd><Kbd><ArrowDown size={8} /></Kbd> navigate</span>
          <span className="ml-auto">{results.length > 0 ? `${results.length} results` : 'FTS5 · SQLite index'}</span>
        </div>
      </div>
    </div>
  )
}
