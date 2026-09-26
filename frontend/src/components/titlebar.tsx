import { useBackendVersion } from '@/lib/backend'

/**
 * Full-width title bar band — the native traffic lights live in this row,
 * above all app UI (matches the 38px invisible title bar in main.go).
 * Window title centered, backend version chip at the right.
 */
export function TitleBar() {
  const { version, presence } = useBackendVersion()
  return (
    <div className="relative flex h-[38px] shrink-0 items-center justify-center border-b border-hairline bg-panel/60">
      <span className="text-[12.5px] font-semibold tracking-tight text-ink">AirDrive</span>
      <span
        className="absolute right-3 font-mono text-[9.5px] text-ink-faint"
        title={
          presence === 'wails'
            ? 'backend: Wails v3 (Go service)'
            : 'backend: browser mock — run under wails3 for the real loop'
        }
      >
        {version}
      </span>
    </div>
  )
}
