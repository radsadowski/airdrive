import { Cloud, Globe, HardDrive, Plus } from 'lucide-react'
import { useUiStore } from '@/stores/ui'
import { formatBytes } from '@/lib/format'
import { useAppStore } from '@/stores/app'

export function Onboarding() {
  const openWizard = useUiStore((s) => s.openWizard)
  const locations = useAppStore((s) => s.locations)
  const totalFiles = locations.reduce((a, l) => a + l.fileCount, 0)
  const totalSize = locations.reduce((a, l) => a + l.totalSize, 0)

  return (
    <div className="relative flex flex-1 items-center justify-center overflow-hidden">
      {/* atmosphere */}
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            'radial-gradient(600px 320px at 50% 30%, rgba(245,166,35,0.07), transparent 70%), radial-gradient(400px 260px at 70% 80%, rgba(92,167,228,0.05), transparent 70%)',
        }}
      />
      <div
        className="pointer-events-none absolute inset-0 opacity-[0.35]"
        style={{
          backgroundImage:
            'linear-gradient(rgba(255,255,255,0.025) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.025) 1px, transparent 1px)',
          backgroundSize: '44px 44px',
          maskImage: 'radial-gradient(500px 360px at 50% 40%, black, transparent)',
        }}
      />

      <div className="relative w-[440px] max-w-[calc(100vw-48px)] text-center">
        <div className="mx-auto flex size-14 items-center justify-center rounded-[14px] border border-amber/25 bg-amber/[0.06]">
          <span className="font-mono text-[20px] font-bold text-amber">A</span>
        </div>
        <h1 className="mt-5 text-[24px] font-bold tracking-tight text-ink">
          One cockpit for every storage you own
        </h1>
        <p className="mx-auto mt-2.5 max-w-[360px] text-[13px] leading-relaxed text-ink-mut">
          Local folders, Google Drive and S3-compatible buckets — browse, search and
          migrate between them. Nothing leaves this machine except your files.
        </p>

        <button
          onClick={() => openWizard('picker')}
          className="group mx-auto mt-7 flex items-center gap-2 rounded-[9px] bg-amber px-5 py-2.5 text-[13.5px] font-semibold text-on-amber shadow-[0_0_32px_-6px_rgba(245,166,35,0.45)] transition-transform hover:scale-[1.03] active:scale-[0.98]"
        >
          <Plus size={15} />
          Connect your first storage
        </button>

        <div className="mt-9 grid grid-cols-3 gap-2">
          {[
            { icon: HardDrive, label: 'Local folders', sub: 'fsnotify watched' },
            { icon: Cloud, label: 'Google Drive', sub: 'OAuth + cursor sync' },
            { icon: Globe, label: 'S3 compatible', sub: 'R2 · B2 · MinIO' },
          ].map((c) => (
            <div key={c.label} className="rounded-[9px] border border-hairline bg-panel/60 px-3 py-3">
              <c.icon size={16} className="mx-auto text-ink-faint" strokeWidth={1.6} />
              <div className="mt-1.5 text-[11.5px] font-medium text-ink-mut">{c.label}</div>
              <div className="mt-0.5 font-mono text-[9px] text-ink-faint">{c.sub}</div>
            </div>
          ))}
        </div>

        {locations.length > 0 && (
          <div className="mt-6 font-mono text-[10px] uppercase tracking-[0.14em] text-ink-faint">
            {locations.length} storages · {totalFiles.toLocaleString()} files · {formatBytes(totalSize)} indexed
          </div>
        )}
      </div>
    </div>
  )
}
