/**
 * Fake-data engine — seeded, deterministic, realistic.
 * Doubles as the fixture set for Vitest + Playwright later.
 */

import type { FileCategory, FileInfo, PreviewKind, ProviderKind } from '../contract'
import { fakeMd5 } from '@/lib/format'

/* ------------------------------------------------------------------ */
/* Seeded RNG (mulberry32)                                              */
/* ------------------------------------------------------------------ */

export class Rng {
  private s: number
  constructor(seed: number) {
    this.s = seed >>> 0
  }
  next(): number {
    this.s = (this.s + 0x6d2b79f5) >>> 0
    let t = this.s
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
  int(min: number, max: number): number {
    return Math.floor(this.next() * (max - min + 1)) + min
  }
  pick<T>(arr: readonly T[]): T {
    return arr[this.int(0, arr.length - 1)]
  }
  /** weighted pick: [item, weight][] */
  weighted<T>(entries: readonly (readonly [T, number])[]): T {
    const total = entries.reduce((a, [, w]) => a + w, 0)
    let r = this.next() * total
    for (const [item, w] of entries) {
      r -= w
      if (r <= 0) return item
    }
    return entries[entries.length - 1][0]
  }
  shuffle<T>(arr: T[]): T[] {
    for (let i = arr.length - 1; i > 0; i--) {
      const j = this.int(0, i)
      ;[arr[i], arr[j]] = [arr[j], arr[i]]
    }
    return arr
  }
}

/* ------------------------------------------------------------------ */
/* Vocabulary                                                           */
/* ------------------------------------------------------------------ */

const FOLDER_WORDS = [
  'Photos', 'Documents', 'Backups', 'Projects', 'Archive', 'Media', 'Downloads',
  'Finance', 'Scans', 'Design', 'Video', 'Music', '2024', '2025', '2026',
  'Invoices', 'Contracts', 'Screenshots', 'Exports', 'Work', 'Personal',
  'Camera', 'RAW', 'Exports', 'Website', 'Mobile', 'Research', 'Notes',
] as const

const FILE_STEMS = [
  'report', 'invoice', 'photo', 'img', 'dsc', 'screenshot', 'notes', 'draft',
  'final', 'backup', 'export', 'budget', 'plan', 'contract', 'resume', 'cv',
  'presentation', 'slides', 'data', 'analysis', 'logo', 'poster', 'mockup',
  'render', 'mixdown', 'master', 'session', 'scan', 'receipt', 'statement',
  'archive', 'config', 'readme', 'changelog', 'todo', 'wip', 'untitled',
] as const

const NAME_SUFFIXES = ['', '_final', '_v2', '_v3', '_old', '_new', '_copy', '_edit', '_raw', '_export', '_2024', '_2025', '-1', '-2', '-final2'] as const

const PERSONS = ['jkowalski', 'mnowak', 'azielinski', 'tbrown', 'admin', 'me', 'team'] as const

export const MIME_MAP: Record<string, { mime: string; preview: PreviewKind; category: FileCategory }> = {
  jpg: { mime: 'image/jpeg', preview: 'image', category: 'image' },
  jpeg: { mime: 'image/jpeg', preview: 'image', category: 'image' },
  png: { mime: 'image/png', preview: 'image', category: 'image' },
  gif: { mime: 'image/gif', preview: 'image', category: 'image' },
  webp: { mime: 'image/webp', preview: 'image', category: 'image' },
  heic: { mime: 'image/heic', preview: 'image', category: 'image' },
  svg: { mime: 'image/svg+xml', preview: 'image', category: 'image' },
  mp4: { mime: 'video/mp4', preview: 'video', category: 'video' },
  mov: { mime: 'video/quicktime', preview: 'video', category: 'video' },
  mkv: { mime: 'video/x-matroska', preview: 'video', category: 'video' },
  mp3: { mime: 'audio/mpeg', preview: 'audio', category: 'audio' },
  wav: { mime: 'audio/wav', preview: 'audio', category: 'audio' },
  flac: { mime: 'audio/flac', preview: 'audio', category: 'audio' },
  pdf: { mime: 'application/pdf', preview: 'pdf', category: 'pdf' },
  doc: { mime: 'application/msword', preview: 'binary', category: 'document' },
  docx: { mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', preview: 'binary', category: 'document' },
  xls: { mime: 'application/vnd.ms-excel', preview: 'binary', category: 'document' },
  xlsx: { mime: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', preview: 'binary', category: 'document' },
  ppt: { mime: 'application/vnd.ms-powerpoint', preview: 'binary', category: 'document' },
  key: { mime: 'application/x-iwork-keynote-sffkey', preview: 'binary', category: 'document' },
  txt: { mime: 'text/plain', preview: 'text', category: 'document' },
  md: { mime: 'text/markdown', preview: 'text', category: 'document' },
  csv: { mime: 'text/csv', preview: 'text', category: 'document' },
  json: { mime: 'application/json', preview: 'text', category: 'code' },
  ts: { mime: 'text/typescript', preview: 'text', category: 'code' },
  tsx: { mime: 'text/typescript', preview: 'text', category: 'code' },
  go: { mime: 'text/x-go', preview: 'text', category: 'code' },
  py: { mime: 'text/x-python', preview: 'text', category: 'code' },
  rs: { mime: 'text/rust', preview: 'text', category: 'code' },
  sh: { mime: 'text/x-sh', preview: 'text', category: 'code' },
  yml: { mime: 'text/yaml', preview: 'text', category: 'code' },
  zip: { mime: 'application/zip', preview: 'binary', category: 'archive' },
  tar: { mime: 'application/x-tar', preview: 'binary', category: 'archive' },
  gz: { mime: 'application/gzip', preview: 'binary', category: 'archive' },
  dmg: { mime: 'application/x-apple-diskimage', preview: 'binary', category: 'archive' },
}

/** per-extension realistic size ranges in bytes */
const SIZE_RANGES: Record<string, [number, number]> = {
  jpg: [400_000, 9_000_000], jpeg: [400_000, 9_000_000], png: [30_000, 4_000_000],
  gif: [80_000, 12_000_000], webp: [60_000, 3_000_000], heic: [700_000, 5_000_000],
  svg: [2_000, 90_000], mp4: [20_000_000, 4_000_000_000], mov: [60_000_000, 8_000_000_000],
  mkv: [200_000_000, 12_000_000_000], mp3: [3_000_000, 20_000_000], wav: [30_000_000, 200_000_000],
  flac: [20_000_000, 120_000_000], pdf: [100_000, 40_000_000], doc: [40_000, 8_000_000],
  docx: [20_000, 12_000_000], xls: [30_000, 6_000_000], xlsx: [15_000, 9_000_000],
  ppt: [100_000, 60_000_000], key: [200_000, 300_000_000], txt: [200, 400_000],
  md: [200, 90_000], csv: [1_000, 60_000_000], json: [500, 8_000_000],
  ts: [500, 300_000], tsx: [800, 250_000], go: [400, 200_000], py: [400, 350_000],
  rs: [400, 300_000], sh: [200, 20_000], yml: [300, 60_000],
  zip: [5_000_000, 2_000_000_000], tar: [10_000_000, 5_000_000_000],
  gz: [1_000_000, 800_000_000], dmg: [50_000_000, 10_000_000_000],
}

/** extension pools per storage persona */
const EXTS = {
  personal: ['jpg', 'jpg', 'jpeg', 'heic', 'png', 'png', 'mp4', 'mov', 'pdf', 'docx', 'xlsx', 'txt', 'md', 'zip', 'mp3', 'dmg', 'json'],
  photoheavy: ['jpg', 'jpg', 'jpg', 'jpeg', 'heic', 'raw', 'png', 'mp4', 'mov', 'psd', 'zip'],
  dev: ['ts', 'tsx', 'go', 'py', 'rs', 'json', 'yml', 'sh', 'md', 'txt', 'zip', 'gz', 'png', 'pdf'],
  media: ['mp4', 'mkv', 'mov', 'mp3', 'flac', 'wav', 'jpg', 'png', 'zip', 'tar'],
  docs: ['pdf', 'docx', 'xlsx', 'ppt', 'key', 'csv', 'txt', 'md', 'zip'],
} as const

type Persona = keyof typeof EXTS

export function fileMeta(name: string): { ext: string; mime: string; preview: PreviewKind; category: FileCategory } {
  const ext = name.includes('.') ? name.split('.').pop()!.toLowerCase() : ''
  const m = MIME_MAP[ext]
  if (m) return { ext, ...m }
  return { ext, mime: 'application/octet-stream', preview: 'binary', category: 'other' }
}

/* ------------------------------------------------------------------ */
/* Tree generation                                                      */
/* ------------------------------------------------------------------ */

let idCounter = 0
export function nextId(prefix: string): string {
  idCounter += 1
  return `${prefix}_${idCounter.toString(36)}`
}

export interface GenDirSpec {
  name: string
  /** approximate total files within (recursive) */
  files: number
  /** max subfolders */
  subdirs?: number
  depth?: number
  persona?: Persona
  /** override: exactly these subfolder names */
  subNames?: string[]
}

export function makeDir(locationId: string, parentId: string | null, name: string, seedBase: string): FileInfo {
  return {
    id: nextId('d'),
    locationId,
    parentId,
    name,
    kind: 'dir',
    size: 0,
    mime: 'inode/directory',
    modifiedAt: new Date().toISOString(),
    etag: fakeMd5(seedBase + '/dir'),
  }
}

export function makeFile(locationId: string, parentId: string | null, name: string, rng: Rng, seedBase: string): FileInfo {
  const meta = fileMeta(name)
  const [lo, hi] = SIZE_RANGES[meta.ext] ?? [1_000, 5_000_000]
  const size = Math.round(lo + rng.next() ** 2.2 * (hi - lo))
  const daysAgo = Math.floor(rng.next() ** 1.6 * 1500)
  const modified = new Date(Date.now() - daysAgo * 86_400_000 - rng.int(0, 86_399_000))
  const f: FileInfo = {
    id: nextId('f'),
    locationId,
    parentId,
    name,
    kind: 'file',
    size,
    mime: meta.mime,
    modifiedAt: modified.toISOString(),
    etag: fakeMd5(seedBase + '/' + name + size),
    previewKind: meta.preview,
  }
  if (meta.preview === 'image') {
    f.thumbnailUrl = `https://picsum.photos/seed/${encodeURIComponent(f.id)}/256/256`
  }
  return f
}

function genName(rng: Rng, persona: Persona): string {
  const ext = rng.pick(EXTS[persona])
  const stem = rng.pick(FILE_STEMS)
  const suffix = rng.next() < 0.45 ? rng.pick(NAME_SUFFIXES) : ''
  const num = rng.next() < 0.3 ? rng.int(1000, 9999).toString() : ''
  const person = rng.next() < 0.15 ? `${rng.pick(PERSONS)}-` : ''
  return `${person}${stem}${suffix}${num}.${ext}`
}

export function genSubtree(
  locationId: string,
  parentId: string | null,
  spec: GenDirSpec,
  out: FileInfo[],
  rng: Rng,
  seedPath: string,
): FileInfo {
  const dir = makeDir(locationId, parentId, spec.name, seedPath)
  out.push(dir)
  const depth = spec.depth ?? 0
  let filesLeft = spec.files

  // direct files — between 0% and 40% of budget at this level
  const direct = Math.min(filesLeft, rng.int(0, Math.ceil(spec.files * 0.4)))
  for (let i = 0; i < direct; i++) {
    out.push(makeFile(locationId, dir.id, genName(rng, spec.persona ?? 'personal'), rng, seedPath))
  }
  filesLeft -= direct

  const subCount = Math.min(spec.subdirs ?? rng.int(1, 4), filesLeft)
  if (subCount > 0 && depth < 3) {
    const per = Math.floor(filesLeft / subCount)
    for (let i = 0; i < subCount; i++) {
      const isLast = i === subCount - 1
      const budget = isLast ? filesLeft : per + (i < filesLeft % subCount ? 1 : 0)
      if (budget <= 0) continue
      const subName = spec.subNames?.[i] ?? rng.pick(FOLDER_WORDS)
      genSubtree(locationId, dir.id, { ...spec, name: subName, files: budget, subdirs: rng.int(1, 4) }, out, rng, `${seedPath}/${subName}`)
      filesLeft -= budget
    }
  } else {
    // depth exhausted — dump remaining as flat files
    for (let i = 0; i < filesLeft; i++) {
      out.push(makeFile(locationId, dir.id, genName(rng, spec.persona ?? 'personal'), rng, seedPath))
    }
  }
  return dir
}

/* ------------------------------------------------------------------ */
/* Location presets                                                     */
/* ------------------------------------------------------------------ */

export interface LocationPreset {
  kind: ProviderKind
  name: string
  root: string
  seed: number
  specs: GenDirSpec[]
}

export const PRESETS: LocationPreset[] = [
  {
    kind: 'localfs',
    name: 'MacBook — Documents',
    root: '/Users/you/Documents',
    seed: 1101,
    specs: [
      { name: 'Documents', files: 340, persona: 'docs' },
      { name: 'Projects', files: 520, persona: 'dev', subdirs: 6 },
      { name: 'Invoices 2026', files: 64, persona: 'docs' },
      { name: 'Scans', files: 120, persona: 'docs' },
    ],
  },
  {
    kind: 'localfs',
    name: 'Studio — Media Drive',
    root: '/Volumes/Studio',
    seed: 1102,
    specs: [
      { name: 'Photo Library', files: 2600, persona: 'photoheavy', subdirs: 8 },
      { name: 'Video Projects', files: 410, persona: 'media', subdirs: 5 },
      { name: 'Music', files: 780, persona: 'media', subdirs: 4 },
      { name: 'Exports', files: 260, persona: 'media' },
    ],
  },
  {
    kind: 'gdrive',
    name: 'Google Drive — you@gmail.com',
    root: 'My Drive',
    seed: 2201,
    specs: [
      { name: 'My Drive', files: 4200, persona: 'personal', subdirs: 7 },
    ],
  },
  {
    kind: 's3',
    name: 'R2 — airdrive-prod',
    root: 'airdrive-prod',
    seed: 3301,
    specs: [
      { name: 'airdrive-prod', files: 1300, persona: 'personal', subdirs: 6 },
    ],
  },
  {
    kind: 's3',
    name: 'R2 — backups-archive',
    root: 'backups-archive',
    seed: 3302,
    specs: [
      { name: 'backups-archive', files: 480, persona: 'dev', subdirs: 4 },
    ],
  },
]

export const STRESS_PRESET: LocationPreset = {
  kind: 'localfs',
  name: 'Stress Test — 250k files',
  root: '/tmp/stress',
  seed: 9901,
  specs: [
    { name: 'stress-root', files: 250_000, subdirs: 24, persona: 'dev' },
  ],
}

/* ------------------------------------------------------------------ */
/* Fake content for previews                                            */
/* ------------------------------------------------------------------ */

const LOREM = [
  'AirDrive is the single cockpit for every storage you own.',
  'The index lives in SQLite; the journals make transfers resumable.',
  'Every byte streamed between providers is verified with MD5.',
  'Nothing is unrecoverable by default — trash everywhere.',
  'Run the migration twice; the second pass is a no-op.',
  'R2 is the rendezvous. Machines never talk to each other.',
  'The crawl is resumable: kill it at file 400,000 of a million.',
  'Virtual folders are prefixes wearing a trench coat.',
  'One interface, every storage. The UI never knows the difference.',
  'Bandwidth throttling keeps the home network usable.',
  'Idempotent conflicts: skip if identical, keep both if unsure.',
  'The contract is one TypeScript file. Drift is a build error.',
]

export function fakeText(seed: string, lines = 24): string {
  const rng = new Rng(hashStr(seed))
  const out: string[] = []
  for (let i = 0; i < lines; i++) {
    const a = rng.pick(LOREM)
    const b = rng.pick(LOREM)
    out.push(`${a} ${b}`)
  }
  return out.join('\n\n')
}

export function fakePdfDataUri(title: string): string {
  // minimal one-page PDF with a title, hand-rolled
  const esc = (s: string) => s.replace(/([()\\])/g, '\\$1')
  const body = [
    'BT /F1 24 Tf 72 700 Td (AirDrive mock document) Tj ET',
    `BT /F1 12 Tf 72 670 Td (${esc(title)}) Tj ET`,
    'BT /F1 10 Tf 72 640 Td (This PDF was generated by the AirDrive mock layer.) Tj ET',
    'BT /F1 10 Tf 72 624 Td (The real app streams actual PDFs from any provider.) Tj ET',
  ].join('\n')
  const objs = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>',
    `<< /Length ${body.length} >>\nstream\n${body}\nendstream`,
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
  ]
  let pdf = '%PDF-1.4\n'
  const offsets: number[] = []
  objs.forEach((o, i) => {
    offsets.push(pdf.length)
    pdf += `${i + 1} 0 obj\n${o}\nendobj\n`
  })
  const xrefStart = pdf.length
  pdf += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`
  for (const off of offsets) pdf += `${off.toString().padStart(10, '0')} 00000 n \n`
  pdf += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${xrefStart}\n%%EOF`
  // btoa via chunks
  const bytes = new TextEncoder().encode(pdf)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
  return `data:application/pdf;base64,${btoa(bin)}`
}

/** fake media runtime in seconds for a stable-looking player */
export function fakeMediaSeconds(seed: string): number {
  return 45 + (hashStr(seed) % 540)
}

export function hashStr(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

export const FAKE_ACCOUNTS = [
  { email: 'you@gmail.com', name: 'You', avatarHue: 38 },
  { email: 'studio.account@gmail.com', name: 'Studio', avatarHue: 140 },
  { email: 'old.backup.acc@gmail.com', name: 'Old Backup', avatarHue: 210 },
] as const
