import { Gauge, Keyboard, Moon, Settings2, Sun } from 'lucide-react'
import { useUiStore } from '@/stores/ui'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { Kbd } from '@/components/ui/badge'

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b border-hairline py-3 last:border-b-0">
      <div>
        <div className="text-[13px] text-ink">{label}</div>
        {hint && <div className="mt-0.5 text-[11.5px] text-ink-faint">{hint}</div>}
      </div>
      {children}
    </div>
  )
}

export function SettingsDialog() {
  const open = useUiStore((s) => s.settingsOpen)
  const setOpen = useUiStore((s) => s.setSettingsOpen)
  const settings = useUiStore((s) => s.settings)
  const patch = useUiStore((s) => s.patchSettings)

  if (!open) return null

  return (
    <Dialog open onOpenChange={(o) => !o && setOpen(false)}>
      <DialogContent width="max-w-[460px]">
        <div className="flex items-center gap-2.5 px-5 pt-5">
          <Settings2 size={15} className="text-amber" />
          <div className="text-[15px] font-semibold tracking-tight text-ink">Settings</div>
        </div>

        <div className="px-5 py-3">
          <div className="mb-1 flex items-center gap-1.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-ink-faint">
            <Sun size={10} /> appearance
          </div>
          <Row label="Theme">
            <div className="flex items-center gap-1 rounded-[7px] border border-hairline bg-raised p-0.5">
              {(['dark', 'light'] as const).map((t) => (
                <button
                  key={t}
                  onClick={() => void patch({ theme: t })}
                  className={[
                    'flex items-center gap-1.5 rounded-[5px] px-2.5 py-1 text-[12px] transition-colors',
                    settings.theme === t
                      ? 'bg-amber text-on-amber font-medium'
                      : 'text-ink-mut hover:text-ink',
                  ].join(' ')}
                >
                  {t === 'dark' ? <Moon size={11} /> : <Sun size={11} />}
                  {t.charAt(0).toUpperCase() + t.slice(1)}
                </button>
              ))}
            </div>
          </Row>

          <div className="mb-1 mt-3 flex items-center gap-1.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-ink-faint">
            <Gauge size={10} /> transfers
          </div>
          <Row label="Concurrent file transfers" hint="worker pool size — the engine picks this up live">
            <span className="font-mono text-[12px] tabular text-amber">{settings.concurrentTransfers}</span>
          </Row>
          <Row label="Bandwidth limit" hint="keeps the home network usable during migrations">
            <span className="font-mono text-[12px] tabular text-amber">{settings.bandwidthLimitMbps} Mbps</span>
          </Row>
          <Row label="Download folder" hint="one-click downloads land here">
            <span className="font-mono text-[11px] text-ink-mut">{settings.downloadDir}</span>
          </Row>

          <div className="mb-1 mt-3 flex items-center gap-1.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-ink-faint">
            <Keyboard size={10} /> behavior
          </div>
          <Row label="Show hidden files" hint="dotfiles & .DS_Store noise">
            <Switch checked={settings.showHiddenFiles} onCheckedChange={(v) => void patch({ showHiddenFiles: v })} />
          </Row>
        </div>

        <div className="border-t border-hairline px-5 py-2.5">
          <div className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-[10px] text-ink-faint">
            <span><Kbd>⌘F</Kbd> search</span>
            <span><Kbd>⌘C</Kbd>/<Kbd>⌘X</Kbd>/<Kbd>⌘V</Kbd> copy / cut / paste</span>
            <span><Kbd>⌫</Kbd> trash</span>
            <span><Kbd>⌘A</Kbd> select all</span>
            <span><Kbd>↵</Kbd> open</span>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
