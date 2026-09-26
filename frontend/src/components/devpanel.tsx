import { useState } from 'react'
import { Activity, FlaskConical, RotateCw, Trash2, Wifi, WifiOff, Zap } from 'lucide-react'
import { isMock, api } from '@/lib/api'
import { mockClient } from '@/lib/api/mock/client'
import { useAppStore } from '@/stores/app'
import { cn } from '@/lib/utils'

/** the mock's floating dev panel — failure injection, offline, latency, stress */
export function DevPanel() {
  const [open, setOpen] = useState(false)
  const [offline, setOffline] = useState(false)
  const [latency, setLatency] = useState(1)
  const reloadLocations = useAppStore((s) => s.reloadLocations)

  if (!isMock || !('failNextCall' in api)) return null
  const dev = api as typeof mockClient

  const toggleOffline = (v: boolean) => {
    dev.setOffline(v)
    setOffline(v)
  }

  const setLat = (v: number) => {
    dev.setLatency(v)
    setLatency(v)
  }

  return (
    <div className="pointer-events-auto fixed bottom-3 right-3 z-[80]">
      {open ? (
        <div className="w-[248px] animate-pop-in overflow-hidden rounded-[10px] border border-hairline-strong bg-overlay/95 shadow-[0_20px_60px_-12px_rgba(0,0,0,0.85)] backdrop-blur">
          <div className="flex items-center gap-2 border-b border-hairline px-3 py-2">
            <FlaskConical size={12} className="text-fuchsia-300" />
            <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-ink-faint">mock dev panel</span>
            <button onClick={() => setOpen(false)} className="ml-auto text-[10px] text-ink-faint hover:text-ink">hide</button>
          </div>

          <div className="space-y-1 p-2">
            <button
              onClick={() => toggleOffline(!offline)}
              className={cn(
                'flex w-full items-center gap-2.5 rounded-[7px] border px-2.5 py-2 text-[12px] transition-colors',
                offline ? 'border-danger/40 bg-danger/10 text-danger' : 'border-hairline text-ink-mut hover:text-ink',
              )}
            >
              {offline ? <WifiOff size={13} /> : <Wifi size={13} />}
              {offline ? 'offline — remote providers fail' : 'go offline'}
            </button>

            <button
              onClick={() => dev.failNextCall()}
              className="flex w-full items-center gap-2.5 rounded-[7px] border border-hairline px-2.5 py-2 text-[12px] text-ink-mut transition-colors hover:text-ink"
            >
              <Zap size={13} className="text-amber" />
              fail next request + transfer
            </button>

            <div className="rounded-[7px] border border-hairline px-2.5 py-2">
              <div className="flex items-center justify-between text-[12px] text-ink-mut">
                <span className="flex items-center gap-2.5"><Activity size={13} /> latency</span>
                <span className="font-mono text-[10.5px] text-amber">×{latency}</span>
              </div>
              <input
                type="range"
                min={1}
                max={10}
                value={latency}
                onChange={(e) => setLat(Number(e.target.value))}
                className="mt-1.5 w-full accent-amber"
              />
            </div>

            <button
              onClick={() => {
                dev.addStressLocation()
                void reloadLocations()
              }}
              disabled={dev.hasStress()}
              className="flex w-full items-center gap-2.5 rounded-[7px] border border-hairline px-2.5 py-2 text-[12px] text-ink-mut transition-colors hover:text-ink disabled:opacity-40"
            >
              <FlaskConical size={13} />
              {dev.hasStress() ? 'stress location added' : 'add stress location · 250k files'}
            </button>

            <div className="flex gap-1 pt-0.5">
              <button
                onClick={() => {
                  dev.reset()
                  window.location.reload()
                }}
                className="flex flex-1 items-center justify-center gap-1.5 rounded-[7px] border border-hairline px-2 py-1.5 text-[11px] text-ink-mut transition-colors hover:text-ink"
              >
                <RotateCw size={11} /> reset seed
              </button>
              <button
                onClick={() => {
                  dev.clearAll()
                  window.location.reload()
                }}
                className="flex flex-1 items-center justify-center gap-1.5 rounded-[7px] border border-hairline px-2 py-1.5 text-[11px] text-ink-mut transition-colors hover:border-danger/40 hover:text-danger"
              >
                <Trash2 size={11} /> clear all
              </button>
            </div>
          </div>
        </div>
      ) : (
        <button
          onClick={() => setOpen(true)}
          className="flex h-7 items-center gap-1.5 rounded-full border border-hairline bg-panel/90 px-2.5 font-mono text-[10px] text-ink-faint shadow backdrop-blur transition-colors hover:border-fuchsia-400/40 hover:text-fuchsia-300"
        >
          <FlaskConical size={11} />
          dev
        </button>
      )}
    </div>
  )
}
