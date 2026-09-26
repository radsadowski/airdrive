/** Formatting helpers — the data cockpit's readouts. */

export function formatBytes(bytes: number, digits = 1): string {
  if (!Number.isFinite(bytes)) return '—'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(Math.floor(Math.log2(bytes) / 10), units.length - 1)
  const v = bytes / 2 ** (i * 10)
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : digits)} ${units[i]}`
}

export function formatSpeed(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`
}

const dateFmt = new Intl.DateTimeFormat('en', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
})
const dateTimeFmt = new Intl.DateTimeFormat('en', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

export function formatDate(iso: string): string {
  const d = new Date(iso)
  return !Number.isFinite(d.getTime()) || d.getFullYear() < 2000 ? '—' : dateFmt.format(d)
}

export function formatDateTime(iso: string): string {
  return dateTimeFmt.format(new Date(iso))
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return '<1s'
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

export function formatEta(bytesRemaining: number, bytesPerSec: number): string {
  if (bytesPerSec <= 0) return '∞'
  return formatDuration((bytesRemaining / bytesPerSec) * 1000)
}

export function relativeTime(iso: string, now = Date.now()): string {
  const diff = now - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 45) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d}d ago`
  return formatDate(iso)
}

/** "3:41" style duration for media */
export function formatClock(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}

const MONTH_DAYS = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]

/** fake-but-stable "etag" for the mock layer */
export function fakeMd5(seed: string): string {
  let h1 = 0xdeadbeef
  let h2 = 0x41c6ce57
  for (let i = 0; i < seed.length; i++) {
    const ch = seed.charCodeAt(i)
    h1 = Math.imul(h1 ^ ch, 2654435761)
    h2 = Math.imul(h2 ^ ch, 1597334677)
  }
  const hex = (n: number) =>
    Array.from({ length: 4 }, (_, k) => ((n >>> (k * 8)) & 0xff).toString(16).padStart(2, '0'))
      .reverse()
      .join('')
  return `${hex(h1)}${hex(h2)}${hex(h1 ^ h2)}${hex(h2 ^ 0x9e3779b9)}`
}

export { MONTH_DAYS }
