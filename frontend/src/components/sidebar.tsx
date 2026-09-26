import { HardDrive, Plus, RefreshCw, Settings2, Trash2, Cloud, Globe, Loader2, X, PanelLeft } from 'lucide-react'
import { useState } from 'react'
import type { Location, ProviderKind } from '@/lib/api/contract'
import { useAppStore } from '@/stores/app'
import { useUiStore, toastError } from '@/stores/ui'
import { api } from '@/lib/api'
import { dropSelectionOnLocation } from '@/lib/actions'
import { cn } from '@/lib/utils'
import { formatBytes, relativeTime } from '@/lib/format'
import { Tooltip } from '@/components/ui/tooltip'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogBody,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'

const KIND_META: Record<ProviderKind, { icon: typeof HardDrive; label: string; dot: string }> = {
  localfs: { icon: HardDrive, label: 'Local', dot: 'bg-loc-local' },
  gdrive: { icon: Cloud, label: 'Google Drive', dot: 'bg-loc-gdrive' },
  s3: { icon: Globe, label: 'S3 compatible', dot: 'bg-loc-s3' },
}

function LocationRow({ loc, collapsed }: { loc: Location; collapsed: boolean }) {
  const current = useAppStore((s) => s.currentLocationId === loc.id)
  const selectLocation = useAppStore((s) => s.selectLocation)
  const reloadLocations = useAppStore((s) => s.reloadLocations)
  const crawl = useAppStore((s) => s.crawlProgress[loc.id])
  const [dropTarget, setDropTarget] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const meta = KIND_META[loc.kind]
  const Icon = loc.status === 'indexing' || loc.status === 'syncing' ? Loader2 : meta.icon

  const statusTitle =
    loc.status === 'error' ? loc.error :
      loc.status === 'offline' ? 'offline' :
        loc.lastSyncedAt ? `synced ${relativeTime(loc.lastSyncedAt)}` :
          'never synced'

  const handleRemove = async () => {
    console.log('[sidebar] removing location', { id: loc.id, name: loc.name, kind: loc.kind })
    setConfirmOpen(false)
    setRemoving(true)
    try {
      await api.removeLocation(loc.id)
      console.log('[sidebar] location removed', loc.id)
      if (current) void selectLocation(null)
      void reloadLocations()
    } catch (e) {
      console.error('[sidebar] failed to remove location', loc.id, e)
      toastError(e)
    } finally {
      setRemoving(false)
    }
  }

  const row = collapsed ? (
    <button
      title={`${loc.name} — ${statusTitle}`}
      onClick={() => void selectLocation(loc.id)}
      onDragOver={(e) => {
        if (!current) {
          e.preventDefault()
          setDropTarget(true)
        }
      }}
      onDragLeave={() => setDropTarget(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDropTarget(false)
        const s = useAppStore.getState()
        if (s.currentLocationId && s.currentLocationId !== loc.id && s.selection.length > 0) {
          dropSelectionOnLocation(loc.id, [...s.selection], s.currentLocationId)
        }
      }}
      className={cn(
        'group relative flex w-full items-center justify-center rounded-[7px] py-2.5 transition-colors animate-rise',
        current ? 'bg-amber/10 text-ink' : 'text-ink-mut hover:bg-white/[0.045] hover:text-ink',
        dropTarget && 'bg-amber/15 ring-1 ring-amber/50',
        removing && 'opacity-50 pointer-events-none',
      )}
    >
      {current && <span className="absolute left-0 top-1/2 h-4 w-[2px] -translate-y-1/2 rounded-r bg-amber" />}
      <span className={cn(loc.status === 'indexing' || loc.status === 'syncing' ? 'text-amber animate-spin-slow' : '')}>
        <Icon size={16} strokeWidth={1.7} />
      </span>
      <span
        className={cn(
          'absolute bottom-1 right-1 size-[6px] rounded-full ring-2 ring-panel',
          meta.dot,
          loc.status === 'error' && 'bg-danger',
        )}
      />
      {crawl && (
        <span className="absolute inset-x-1.5 bottom-0 h-[2px] overflow-hidden rounded-full bg-white/10">
          <span
            className={cn(
              'block h-full rounded-full bg-amber transition-[width] duration-200',
              crawl.total <= 0 && 'w-2/5 animate-pulse',
            )}
            style={crawl.total > 0 ? { width: `${(crawl.done / crawl.total) * 100}%` } : undefined}
          />
        </span>
      )}
    </button>
  ) : (
    <button
      title={statusTitle}
      onClick={() => void selectLocation(loc.id)}
      onDragOver={(e) => {
        if (!current) {
          e.preventDefault()
          setDropTarget(true)
        }
      }}
      onDragLeave={() => setDropTarget(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDropTarget(false)
        const s = useAppStore.getState()
        if (s.currentLocationId && s.currentLocationId !== loc.id && s.selection.length > 0) {
          dropSelectionOnLocation(loc.id, [...s.selection], s.currentLocationId)
        }
      }}
      className={cn(
        'group relative flex w-full items-center gap-2.5 rounded-[7px] px-2.5 py-2 text-left transition-colors animate-rise',
        current ? 'bg-amber/10 text-ink' : 'text-ink-mut hover:bg-white/[0.045] hover:text-ink',
        dropTarget && 'bg-amber/15 ring-1 ring-amber/50',
        removing && 'opacity-50 pointer-events-none',
      )}
    >
      {current && <span className="absolute left-0 top-1/2 h-4 w-[2px] -translate-y-1/2 rounded-r bg-amber" />}
      <span className={cn('size-[7px] shrink-0 rounded-full', meta.dot, loc.status === 'error' && 'bg-danger')} />
      <span className={cn('shrink-0', loc.status === 'indexing' || loc.status === 'syncing' ? 'text-amber animate-spin-slow' : '')}>
        <Icon size={15} strokeWidth={1.7} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[12.5px] font-medium leading-tight">{loc.name}</span>
        <span className="mt-0.5 block truncate font-mono text-[10.5px] leading-tight text-ink-faint">
          {crawl
            ? `${(crawl.done / 1000).toFixed(1)}k / ${(crawl.total / 1000).toFixed(1)}k indexed`
            : `${loc.fileCount.toLocaleString()} files · ${formatBytes(loc.totalSize)}`}
        </span>
      </span>
      <span
        role="button"
        tabIndex={-1}
        title="Refresh index"
        onClick={(e) => {
          e.stopPropagation()
          void api.refreshLocation(loc.id).catch(() => undefined)
        }}
        className="hidden shrink-0 rounded p-1 text-ink-faint transition-colors hover:bg-white/10 hover:text-ink group-hover:block"
      >
        <RefreshCw size={12} />
      </span>
      {crawl && (
        <span className="absolute inset-x-2.5 bottom-0.5 h-[2px] overflow-hidden rounded-full bg-white/10">
          <span
            className="block h-full rounded-full bg-amber transition-[width] duration-200"
            style={{ width: `${(crawl.done / Math.max(1, crawl.total)) * 100}%` }}
          />
        </span>
      )}
    </button>
  )

  return (
    <>
    <ContextMenu>
      <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onSelect={() => void api.refreshLocation(loc.id).catch(() => undefined)}>
          <RefreshCw size={13} /> Refresh index
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem danger onSelect={() => setConfirmOpen(true)}>
          <X size={13} /> Remove location
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>

    {confirmOpen && (
      <Dialog open onOpenChange={(open) => !open && setConfirmOpen(false)}>
        <DialogContent width="max-w-sm">
          <DialogHeader>
            <DialogTitle>Remove location?</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-[13px] leading-relaxed text-ink-mut">
              <span className="font-medium text-ink">{loc.name}</span> will be disconnected from AirDrive and its index will be cleared.
            </p>
            <p className="mt-2 text-[12px] text-ink-faint">
              Your actual files are not affected — nothing is deleted from disk or cloud storage.
            </p>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>Cancel</Button>
            <Button variant="danger" disabled={removing} onClick={() => void handleRemove()}>
              {removing ? <Loader2 size={13} className="animate-spin" /> : <X size={13} />}
              Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )}
    </>
  )
}

export function Sidebar() {
  const locations = useAppStore((s) => s.locations)
  const openWizard = useUiStore((s) => s.openWizard)
  const setSettingsOpen = useUiStore((s) => s.setSettingsOpen)
  const sidebarOpen = useUiStore((s) => s.sidebarOpen)
  const toggleSidebar = useUiStore((s) => s.toggleSidebar)
  const collapsed = !sidebarOpen

  const groups: { label: string; kinds: ProviderKind[] }[] = [
    { label: 'On this Mac', kinds: ['localfs'] },
    { label: 'Cloud', kinds: ['gdrive', 's3'] },
  ]

  return (
    <aside
      className={cn(
        'flex h-full shrink-0 flex-col border-r border-hairline bg-panel/60 transition-[width] duration-150',
        collapsed ? 'w-[52px]' : 'w-[228px]',
      )}
    >
      <div className={cn('flex-1 overflow-y-auto overflow-x-hidden py-2', collapsed ? 'px-1.5' : 'px-2')}>
        {locations.length === 0 && !collapsed && (
          <div className="px-2.5 py-6 text-center text-[12px] leading-relaxed text-ink-faint">
            No storages connected.
            <br />
            Add one below.
          </div>
        )}
        {groups.map((g) => {
          const locs = locations.filter((l) => g.kinds.includes(l.kind))
          if (locs.length === 0) return null
          return (
            <div key={g.label} className="mb-2">
              {!collapsed && (
                <div className="px-2.5 pb-1 pt-2 font-mono text-[9.5px] uppercase tracking-[0.14em] text-ink-faint">
                  {g.label}
                </div>
              )}
              <div className="flex flex-col gap-0.5">
                {locs.map((loc) => (
                  <LocationRow key={loc.id} loc={loc} collapsed={collapsed} />
                ))}
              </div>
            </div>
          )
        })}
      </div>

      <div className={cn('border-t border-hairline p-2', collapsed && 'px-1.5')}>
        {!collapsed && (
          <>
            <button
              onClick={() => openWizard('picker')}
              className="flex w-full items-center gap-2 rounded-[7px] border border-dashed border-hairline-strong px-2.5 py-2 text-[12.5px] text-ink-mut transition-colors hover:border-amber/50 hover:text-amber"
            >
              <Plus size={14} />
              Add storage
            </button>
            <div className="mt-1 flex">
              <button
                onClick={() => setSettingsOpen(true)}
                className="flex flex-1 items-center gap-2 rounded-[7px] px-2.5 py-1.5 text-[12px] text-ink-faint transition-colors hover:bg-white/[0.045] hover:text-ink-mut"
              >
                <Settings2 size={13} /> Settings
              </button>
              <Tooltip label="Trash browsing lands with the real shell (Phase 2)" side="top">
                <span className="flex items-center rounded-[7px] px-2 py-1.5 font-mono text-[10px] text-ink-faint">
                  <Trash2 size={13} className="mr-1" /> Trash
                </span>
              </Tooltip>
            </div>
          </>
        )}
        {collapsed && (
          <Tooltip label="Add storage" side="right">
            <button
              onClick={() => openWizard('picker')}
              className="flex w-full items-center justify-center rounded-[7px] border border-dashed border-hairline-strong py-2 text-ink-mut transition-colors hover:border-amber/50 hover:text-amber"
            >
              <Plus size={14} />
            </button>
          </Tooltip>
        )}

        <Tooltip label={`${collapsed ? 'Expand' : 'Collapse'} sidebar  ⌘E`} side="right">
          <button
            onClick={toggleSidebar}
            className={cn(
              'mt-1 flex items-center gap-2 rounded-[7px] px-2.5 py-1.5 text-[12px] text-ink-faint transition-colors hover:bg-white/[0.045] hover:text-ink-mut',
              collapsed ? 'w-full justify-center' : 'w-full',
            )}
          >
            <PanelLeft size={13} className={cn(collapsed && 'rotate-180')} />
            {!collapsed && 'Collapse'}
          </button>
        </Tooltip>
      </div>
    </aside>
  )
}
