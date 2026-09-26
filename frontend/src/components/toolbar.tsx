import { ChevronLeft, ChevronRight, LayoutGrid, List, Search, FolderPlus, ArrowUpDown } from 'lucide-react'
import type { SortField, SortSpec } from '@/lib/api/contract'
import { useAppStore } from '@/stores/app'
import { useUiStore } from '@/stores/ui'
import { newFolder } from '@/lib/actions'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Kbd } from '@/components/ui/badge'
import { Tooltip } from '@/components/ui/tooltip'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const SORTS: { field: SortField; label: string }[] = [
  { field: 'name', label: 'Name' },
  { field: 'size', label: 'Size' },
  { field: 'modifiedAt', label: 'Date modified' },
  { field: 'kind', label: 'Type' },
]

export function Toolbar() {
  const breadcrumbs = useAppStore((s) => s.breadcrumbs)
  const navigateTo = useAppStore((s) => s.navigateTo)
  const goBack = useAppStore((s) => s.goBack)
  const goForward = useAppStore((s) => s.goForward)
  const canGoBack = useAppStore((s) => s.canGoBack())
  const canGoForward = useAppStore((s) => s.canGoForward())
  const sort = useAppStore((s) => s.sort)
  const setSort = useAppStore((s) => s.setSort)
  const view = useAppStore((s) => s.view)
  const setView = useAppStore((s) => s.setView)
  const selection = useAppStore((s) => s.selection)
  const items = useAppStore((s) => s.items)
  const setSearchOpen = useUiStore((s) => s.setSearchOpen)
  const hasLocation = useAppStore((s) => s.currentLocationId !== null)

  return (
    <div className="flex h-11 shrink-0 items-center gap-1 border-b border-hairline bg-panel/40 px-3">
      {/* back / forward */}
      <Tooltip label="Back ⌘[" side="bottom">
        <Button
          variant="ghost"
          size="icon"
          onClick={() => void goBack()}
          disabled={!canGoBack}
          className="shrink-0"
        >
          <ChevronLeft />
        </Button>
      </Tooltip>
      <Tooltip label="Forward ⌘]" side="bottom">
        <Button
          variant="ghost"
          size="icon"
          onClick={() => void goForward()}
          disabled={!canGoForward}
          className="shrink-0"
        >
          <ChevronRight />
        </Button>
      </Tooltip>

      <div className="mx-0.5 h-4 w-px bg-hairline" />

      {/* breadcrumbs */}
      <nav className="flex min-w-0 flex-1 items-center overflow-hidden text-[13px]">
        {breadcrumbs.map((crumb, i) => {
          const isLast = i === breadcrumbs.length - 1
          return (
            <span key={`${crumb.id}-${i}`} className="flex shrink-0 items-center">
              {i > 0 && <ChevronRight size={12} className="mx-0.5 shrink-0 text-ink-faint" />}
              <button
                onClick={() => void navigateTo(i)}
                className={cn(
                  'max-w-[220px] truncate rounded px-1.5 py-0.5 transition-colors',
                  isLast ? 'font-medium text-ink' : 'text-ink-mut hover:bg-white/5 hover:text-ink',
                )}
              >
                {crumb.name}
              </button>
            </span>
          )
        })}
        {selection.length > 0 && (
          <span className="ml-2 shrink-0 font-mono text-[10.5px] text-amber">
            {selection.length} of {items.length} selected
          </span>
        )}
      </nav>

      {/* search trigger */}
      <button
        onClick={() => setSearchOpen(true)}
        className="mr-1 flex h-7 items-center gap-2 rounded-[6px] border border-hairline bg-black/25 pl-2 pr-1 text-[12px] text-ink-faint transition-colors hover:border-amber/40 hover:text-ink-mut"
      >
        <Search size={12.5} />
        <span className="hidden w-20 text-left sm:block">Search</span>
        <Kbd>⌘F</Kbd>
      </button>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" className="gap-1.5">
            <ArrowUpDown size={13} />
            <span className="hidden text-[12px] md:inline">Sort</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuRadioGroup
            value={`${sort.field}:${sort.dir}`}
            onValueChange={(v) => {
              const [field, dir] = v.split(':') as [SortField, 'asc' | 'desc']
              setSort({ field, dir } as SortSpec)
            }}
          >
            {SORTS.map((s) => (
              <div key={s.field}>
                <DropdownMenuRadioItem value={`${s.field}:asc`}>{s.label} · asc</DropdownMenuRadioItem>
                <DropdownMenuRadioItem value={`${s.field}:desc`}>{s.label} · desc</DropdownMenuRadioItem>
              </div>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      <div className="mx-0.5 h-4 w-px bg-hairline" />

      <Button variant="ghost" size="icon" onClick={() => setView(view === 'list' ? 'grid' : 'list')}>
        {view === 'list' ? <LayoutGrid /> : <List />}
      </Button>
      <Button variant="ghost" size="icon" onClick={newFolder} disabled={!hasLocation}>
        <FolderPlus />
      </Button>
    </div>
  )
}
