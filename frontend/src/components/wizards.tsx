import { useEffect, useRef, useState } from 'react'
import * as AppSvc from '@bindings/github.com/radosny/airdrive/internal/bridge/appservice'
import {
  ArrowLeft,
  Check,
  ChevronRight,
  Cloud,
  Folder,
  Globe,
  HardDrive,
  Loader2,
  ShieldCheck,
  Star,
} from 'lucide-react'
import type { FileInfo, GdriveSession, ProviderKind, S3LocationConfig } from '@/lib/api/contract'
import { api, isMock } from '@/lib/api'
import { useAppStore } from '@/stores/app'
import { useUiStore } from '@/stores/ui'
import { cn } from '@/lib/utils'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'

/* ----------------------------- picker ----------------------------- */

const PROVIDERS: { kind: ProviderKind | 'trash'; title: string; desc: string; icon: typeof HardDrive; soon?: boolean }[] = [
  { kind: 'localfs', title: 'Local folder', desc: 'Any folder on this Mac — indexed & watched', icon: HardDrive },
  { kind: 'gdrive', title: 'Google Drive', desc: 'Sign in with Google · My Drive', icon: Cloud },
  { kind: 's3', title: 'S3 compatible', desc: 'R2 · B2 · MinIO · AWS — any bucket', icon: Globe },
  { kind: 'trash', title: 'Dropbox / OneDrive', desc: 'Planned post-v1', icon: Globe, soon: true },
]

function Picker({ onPick }: { onPick: (k: ProviderKind) => void }) {
  return (
    <div className="grid gap-1.5 p-2">
      {PROVIDERS.map((p) => (
        <button
          key={p.title}
          disabled={p.soon}
          onClick={() => !p.soon && onPick(p.kind as ProviderKind)}
          className={cn(
            'group flex items-center gap-3.5 rounded-[9px] border border-hairline p-3 text-left transition-all',
            p.soon ? 'cursor-default opacity-45' : 'hover:border-amber/40 hover:bg-amber/[0.05]',
          )}
        >
          <span className="flex size-10 shrink-0 items-center justify-center rounded-[8px] border border-hairline bg-white/[0.03] text-ink-mut group-hover:text-amber">
            <p.icon size={19} strokeWidth={1.6} />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block text-[13.5px] font-medium text-ink">{p.title}</span>
            <span className="mt-0.5 block text-[12px] text-ink-faint">{p.desc}</span>
          </span>
          {p.soon && <Badge>soon</Badge>}
        </button>
      ))}
    </div>
  )
}

/* ----------------------------- local ----------------------------- */

function LocalWizard() {
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [picking, setPicking] = useState(false)
  const openWizard = useUiStore((s) => s.openWizard)
  const selectLocation = useAppStore((s) => s.selectLocation)
  const inputRef = useRef<HTMLInputElement>(null)

  const browse = async () => {
    setPicking(true)
    try {
      const chosen = await AppSvc.PickFolder()
      if (chosen) {
        setPath(chosen)
        if (!name) setName('')
        inputRef.current?.focus()
      }
    } catch {
      // running in browser mock — no native dialog available
    } finally {
      setPicking(false)
    }
  }

  const valid = path.trim().startsWith('/') && path.trim().split('/').length > 2

  const submit = async () => {
    setBusy(true)
    try {
      const loc = await api.addLocalLocation({ root: path.trim(), name: name.trim() || undefined })
      openWizard(null)
      await selectLocation(loc.id)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4 p-5 pt-1">
      <div>
        <div className="mb-1.5 text-[12px] font-medium text-ink-mut">Folder path</div>
        <div className="flex gap-2">
          <Input
            ref={inputRef}
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/Users/you/Documents"
            className="font-mono text-[12.5px]"
            spellCheck={false}
          />
          <Button variant="secondary" className="shrink-0" onClick={() => void browse()} disabled={picking}>
            {picking ? <Loader2 size={13} className="animate-spin" /> : 'Browse…'}
          </Button>
        </div>

      </div>
      <div>
        <div className="mb-1.5 text-[12px] font-medium text-ink-mut">Display name (optional)</div>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Auto from folder" />
      </div>
      <div className="flex justify-end gap-2 pt-1">
        <Button variant="ghost" onClick={() => openWizard('picker')}>Back</Button>
        <Button variant="primary" disabled={!valid || busy} onClick={() => void submit()}>
          {busy ? <Loader2 size={13} className="animate-spin" /> : <Check size={14} />}
          Add folder
        </Button>
      </div>
    </div>
  )
}

/* ----------------------------- google ----------------------------- */

function GoogleWizard() {
  const connectRef = useRef<{ attempt: number; promise: Promise<GdriveSession> } | null>(null)
  const [session, setSession] = useState<GdriveSession | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const [path, setPath] = useState<{ id: string | null; name: string }[]>([{ id: null, name: 'My Drive' }])
  const [folders, setFolders] = useState<FileInfo[] | null>(null)
  const [starred, setStarred] = useState<FileInfo[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const openWizard = useUiStore((s) => s.openWizard)
  const selectLocation = useAppStore((s) => s.selectLocation)

  const expired = (e: unknown) => /expired/i.test(e instanceof Error ? e.message : String(e))
  const restart = () => {
    setSession(null)
    setFolders(null)
    setPath([{ id: null, name: 'My Drive' }])
    setAttempt((n) => n + 1)
  }

  useEffect(() => {
    // connectGdrive() opens the system browser and starts a real loopback
    // listener — it can't be cancelled once started, and must only run once
    // per attempt. React StrictMode double-invokes effects in dev, so the
    // call is deduped via a ref keyed by `attempt`; every invocation attaches
    // its own .then()/.catch() to the same shared promise.
    if (!connectRef.current || connectRef.current.attempt !== attempt) {
      setError(null)
      connectRef.current = { attempt, promise: api.connectGdrive() }
    }
    const { promise } = connectRef.current

    let cancelled = false
    promise
      .then((sess) => !cancelled && setSession(sess))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      cancelled = true
    }
  }, [attempt])

  const current = path[path.length - 1]

  useEffect(() => {
    if (!session) return
    let cancelled = false
    setLoading(true)
    const browsing = api.browseGdriveFolders(session.sessionId, current.id)
    const starredFetch = current.id === null ? api.starredGdriveFolders(session.sessionId) : Promise.resolve(null)
    Promise.all([browsing, starredFetch])
      .then(([fs, ss]) => {
        if (cancelled) return
        setFolders(fs)
        setStarred(ss)
      })
      .catch((e) => {
        if (cancelled) return
        if (expired(e)) {
          restart()
          return
        }
        setError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [session, current])

  const confirm = async () => {
    if (!session) return
    setBusy(true)
    try {
      const loc = await api.confirmGdriveLocation(session.sessionId, current.id)
      openWizard(null)
      await selectLocation(loc.id)
    } catch (e) {
      if (expired(e)) {
        restart()
        return
      }
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!session) {
    if (error) {
      return (
        <div className="space-y-4 p-5 pt-1">
          <div className="rounded-[8px] border border-danger/30 bg-danger/10 p-3 text-[12.5px] text-danger">{error}</div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => openWizard('picker')}>Back</Button>
            <Button variant="primary" onClick={() => setAttempt((n) => n + 1)}>Try again</Button>
          </div>
        </div>
      )
    }
    return (
      <div className="space-y-4 p-5 pt-1">
        <div className="flex flex-col items-center gap-3 py-10 text-center">
          <Loader2 size={20} className="animate-spin text-ink-faint" />
          <div className="text-[12.5px] text-ink-faint">Waiting for you to finish signing in with Google in your browser…</div>
          <div className="flex items-center gap-1.5 text-[11px] text-ink-faint/80">
            <ShieldCheck size={12} />
            Full Drive access · token stored in your macOS Keychain
          </div>
        </div>
        <div className="flex items-center justify-end pt-1">
          <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>Reopen browser</Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-3 p-5 pt-1">
      <div className="flex items-center justify-between text-[11.5px] text-ink-faint">
        <span>Signed in as <span className="text-ink-mut">{session.email}</span></span>
        <button className="hover:text-ink-mut" onClick={restart}>Switch account</button>
      </div>

      <div className="flex items-center gap-0.5 overflow-x-auto text-[12px]">
        {path.map((p, i) => (
          <div key={p.id ?? 'root'} className="flex shrink-0 items-center">
            {i > 0 && <ChevronRight size={12} className="text-ink-faint/60" />}
            <button
              className={cn('rounded px-1.5 py-0.5 hover:bg-white/[0.05]', i === path.length - 1 ? 'text-ink' : 'text-ink-faint')}
              onClick={() => setPath(path.slice(0, i + 1))}
            >
              {p.name}
            </button>
          </div>
        ))}
      </div>

      <div className="min-h-[180px] max-h-[264px] overflow-y-auto rounded-[8px] border border-hairline">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={16} className="animate-spin text-ink-faint" />
          </div>
        ) : (
          <>
            {current.id === null && starred && starred.length > 0 && (
              <div>
                <div className="px-3 pb-1 pt-2 text-[10.5px] font-medium uppercase tracking-wide text-ink-faint">
                  Starred · incl. Computers
                </div>
                {starred
                  .filter((s) => !folders?.some((f) => f.id === s.id))
                  .map((f) => (
                    <button
                      key={f.id}
                      className="flex w-full items-center gap-2.5 border-b border-hairline/60 px-3 py-2 text-left hover:bg-white/[0.04]"
                      onClick={() => setPath([...path, { id: f.id, name: f.name }])}
                    >
                      <Star size={13} className="shrink-0 fill-amber/70 text-amber/70" />
                      <span className="flex-1 truncate text-[12.5px] text-ink">{f.name}</span>
                      <ChevronRight size={13} className="text-ink-faint/60" />
                    </button>
                  ))}
                <div className="px-3 pb-1 pt-2 text-[10.5px] font-medium uppercase tracking-wide text-ink-faint">My Drive</div>
              </div>
            )}
            {folders && folders.length > 0 ? (
              folders.map((f) => (
                <button
                  key={f.id}
                  className="flex w-full items-center gap-2.5 border-b border-hairline/60 px-3 py-2 text-left last:border-0 hover:bg-white/[0.04]"
                  onClick={() => setPath([...path, { id: f.id, name: f.name }])}
                >
                  <Folder size={14} className="shrink-0 text-amber/80" />
                  <span className="flex-1 truncate text-[12.5px] text-ink">{f.name}</span>
                  <ChevronRight size={13} className="text-ink-faint/60" />
                </button>
              ))
            ) : (
              <div className="flex flex-col items-center gap-1 py-12 text-center">
                <Folder size={16} className="text-ink-faint" />
                <div className="text-[12px] text-ink-faint">No subfolders — sync this folder as-is</div>
              </div>
            )}
          </>
        )}
      </div>

      {error && (
        <div className="rounded-[7px] border border-danger/30 bg-danger/10 px-3 py-2 text-[12px] text-danger">{error}</div>
      )}

      <div className="flex items-center justify-between pt-1">
        <Button
          variant="ghost"
          disabled={path.length < 2 || busy}
          onClick={() => setPath(path.slice(0, -1))}
        >
          <ArrowLeft size={13} /> Up
        </Button>
        <Button variant="primary" disabled={busy} onClick={() => void confirm()}>
          {busy ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
          {path.length === 1 ? 'Sync all of My Drive' : `Sync “${current.name}”`}
        </Button>
      </div>
    </div>
  )
}

/* ------------------------------- s3 ------------------------------- */

const PRESETS: { id: S3LocationConfig['preset']; label: string; hint: string }[] = [
  { id: 'r2', label: 'Cloudflare R2', hint: 'zero egress fees · recommended hub' },
  { id: 'b2', label: 'Backblaze B2', hint: 'cheap + free egress tier' },
  { id: 'minio', label: 'MinIO / self-hosted', hint: 'path-style addressing' },
  { id: 'aws', label: 'AWS S3', hint: 'egress fees apply' },
]

function S3Wizard() {
  const [preset, setPreset] = useState<S3LocationConfig['preset']>('r2')
  const [endpoint, setEndpoint] = useState('')
  const [accessKey, setAccessKey] = useState('')
  const [secretKey, setSecretKey] = useState('')
  const [bucket, setBucket] = useState('')
  const [pathStyle, setPathStyle] = useState(true)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; buckets: string[]; error?: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const openWizard = useUiStore((s) => s.openWizard)
  const selectLocation = useAppStore((s) => s.selectLocation)

  const cfg = (): S3LocationConfig => ({
    preset,
    endpoint: endpoint.trim(),
    region: preset === 'r2' ? 'auto' : preset === 'minio' ? 'us-east-1' : 'us-east-1',
    accessKeyId: accessKey.trim(),
    secretAccessKey: secretKey.trim(),
    bucket: bucket.trim(),
    pathStyle: preset === 'r2' || preset === 'minio' ? true : pathStyle,
  })

  useEffect(() => {
    setTestResult(null)
  }, [preset, accessKey, endpoint])

  const test = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const r = await api.testS3Connection(cfg())
      setTestResult(r)
      if (r.ok && !bucket && r.buckets[0]) setBucket(r.buckets[0])
    } catch (e) {
      setTestResult({ ok: false, buckets: [], error: e instanceof Error ? e.message : String(e) })
    } finally {
      setTesting(false)
    }
  }

  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      const loc = await api.addS3Location(cfg())
      openWizard(null)
      await selectLocation(loc.id)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const valid = testResult?.ok && bucket.trim().length > 0

  return (
    <div className="space-y-4 p-5 pt-1">
      <div className="grid grid-cols-2 gap-1.5">
        {PRESETS.map((p) => (
          <button
            key={p.id}
            onClick={() => setPreset(p.id)}
            className={cn(
              'rounded-[8px] border p-2.5 text-left transition-colors',
              preset === p.id ? 'border-amber/50 bg-amber/[0.07]' : 'border-hairline hover:border-hairline-strong',
            )}
          >
            <div className={cn('text-[12.5px] font-medium', preset === p.id ? 'text-amber' : 'text-ink')}>{p.label}</div>
            <div className="mt-0.5 text-[10.5px] leading-tight text-ink-faint">{p.hint}</div>
          </button>
        ))}
      </div>

      <div className="grid grid-cols-2 gap-2">
        <div className="col-span-2">
          <div className="mb-1 text-[11.5px] font-medium text-ink-mut">Endpoint</div>
          <Input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="https://<account>.r2.cloudflarestorage.com" className="font-mono text-[12px]" spellCheck={false} />
        </div>
        <div>
          <div className="mb-1 text-[11.5px] font-medium text-ink-mut">Access key ID</div>
          <Input value={accessKey} onChange={(e) => setAccessKey(e.target.value)} placeholder="AKIA…" className="font-mono text-[12px]" spellCheck={false} />
        </div>
        <div>
          <div className="mb-1 text-[11.5px] font-medium text-ink-mut">Secret access key</div>
          <Input type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} className="font-mono text-[12px]" spellCheck={false} />
        </div>
        <div>
          <div className="mb-1 text-[11.5px] font-medium text-ink-mut">Bucket</div>
          <Input value={bucket} onChange={(e) => setBucket(e.target.value)} placeholder="airdrive-prod" className="font-mono text-[12px]" spellCheck={false} />
        </div>
        <div className="flex items-end justify-between pb-0.5">
          <label className="flex items-center gap-2 text-[12px] text-ink-mut">
            <Switch checked={cfg().pathStyle} onCheckedChange={setPathStyle} />
            path-style
          </label>
          <span className="font-mono text-[10px] text-ink-faint">region: {cfg().region}</span>
        </div>
      </div>

      {testResult && !testResult.ok && (
        <div className="rounded-[7px] border border-danger/30 bg-danger/10 px-3 py-2 text-[12px] text-danger">{testResult.error}</div>
      )}
      {testResult?.ok && (
        <div className="rounded-[7px] border border-success/25 bg-success/10 px-3 py-2 text-[12px] text-success">
          Connected — visible buckets: <span className="font-mono">{testResult.buckets.join(', ')}</span>
        </div>
      )}
      {error && (
        <div className="rounded-[7px] border border-danger/30 bg-danger/10 px-3 py-2 text-[12px] text-danger">{error}</div>
      )}

      <div className="flex items-center justify-between pt-1">
        <Button variant="ghost" onClick={() => openWizard('picker')}><ArrowLeft size={13} /> Back</Button>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => void test()} disabled={testing || (!accessKey && !endpoint)}>
            {testing ? <Loader2 size={13} className="animate-spin" /> : null}
            Test connection
          </Button>
          <Button variant="primary" disabled={!valid || busy} onClick={() => void submit()}>
            {busy ? <Loader2 size={13} className="animate-spin" /> : <Check size={14} />}
            Connect bucket
          </Button>
        </div>
      </div>
      {isMock && (
        <div className="text-center font-mono text-[10px] text-ink-faint">
          mock: any endpoint + any keys pass the test · use dev panel to inject failures
        </div>
      )}
    </div>
  )
}

/* ------------------------------ shell ------------------------------ */

export function Wizards() {
  const wizard = useUiStore((s) => s.wizard)
  const openWizard = useUiStore((s) => s.openWizard)

  if (!wizard) return null

  const titles: Record<string, { title: string; sub: string }> = {
    picker: { title: 'Add a storage', sub: 'One AirDrive, every storage.' },
    local: { title: 'Add local folder', sub: 'Indexed locally, watched with fsnotify' },
    drive: { title: 'Connect Google Drive', sub: 'OAuth in Testing mode — fine for personal use' },
    s3: { title: 'Connect S3-compatible storage', sub: 'The rendezvous for your machines' },
  }
  const t = titles[wizard]

  return (
    <Dialog open onOpenChange={(open) => !open && openWizard(null)}>
      <DialogContent width={wizard === 'picker' ? 'max-w-[430px]' : 'max-w-[480px]'}>
        <div className="px-5 pt-5">
          <div className="text-[15px] font-semibold tracking-tight text-ink">{t.title}</div>
          <div className="mt-0.5 text-[12.5px] text-ink-faint">{t.sub}</div>
        </div>
        {wizard === 'picker' && <Picker onPick={(k) => openWizard(k === 'localfs' ? 'local' : k === 'gdrive' ? 'drive' : 's3')} />}
        {wizard === 'local' && <LocalWizard />}
        {wizard === 'drive' && <GoogleWizard />}
        {wizard === 's3' && <S3Wizard />}
      </DialogContent>
    </Dialog>
  )
}
