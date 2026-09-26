import { useEffect, useMemo, useState } from 'react'
import { FileText, Play, Volume2 } from 'lucide-react'
import type { FileInfo } from '@/lib/api/contract'
import { fakeMediaSeconds, fakePdfDataUri, fakeText } from '@/lib/api/mock/data'
import { formatClock } from '@/lib/format'

function TextPreview({ file }: { file: FileInfo }) {
  const text = useMemo(() => fakeText(file.id, 18), [file.id])
  return (
    <div className="allow-scroll flex-1 overflow-auto rounded-[8px] border border-hairline bg-black/30 p-3">
      <pre className="whitespace-pre-wrap font-mono text-[11px] leading-relaxed text-ink-mut">{text}</pre>
      <div className="mt-2 text-right font-mono text-[9.5px] text-ink-faint">truncated — full file is 3.2× longer</div>
    </div>
  )
}

function PdfPreview({ file }: { file: FileInfo }) {
  const uri = useMemo(() => fakePdfDataUri(file.name), [file.name])
  return (
    <div className="flex-1 overflow-hidden rounded-[8px] border border-hairline bg-black/30">
      <iframe src={uri} title={file.name} className="size-full" />
    </div>
  )
}

function ImagePreview({ file }: { file: FileInfo }) {
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)
  const src = file.thumbnailUrl?.replace('/256/256', '/640/480')
  if (!src || failed) {
    return (
      <div className="flex flex-1 items-center justify-center rounded-[8px] border border-hairline bg-black/30">
        <FileText size={22} className="text-ink-faint" />
      </div>
    )
  }
  return (
    <div className="relative flex-1 overflow-hidden rounded-[8px] border border-hairline bg-[repeating-conic-gradient(#141519_0%_25%,#191a1f_0%_50%)] bg-[length:16px_16px]">
      {!loaded && <div className="absolute inset-0 animate-pulse bg-white/[0.03]" />}
      <img
        src={src}
        alt={file.name}
        loading="lazy"
        draggable={false}
        onLoad={() => setLoaded(true)}
        onError={() => setFailed(true)}
        className="size-full object-contain"
      />
    </div>
  )
}

function MediaPreview({ file, kind }: { file: FileInfo; kind: 'video' | 'audio' }) {
  const seconds = useMemo(() => fakeMediaSeconds(file.id), [file.id])
  const [playing, setPlaying] = useState(false)
  const [t, setT] = useState(0)

  useEffect(() => {
    if (!playing) return
    const iv = setInterval(() => {
      setT((v) => {
        if (v >= seconds) {
          setPlaying(false)
          return 0
        }
        return v + 1
      })
    }, 1000)
    return () => clearInterval(iv)
  }, [playing, seconds])

  if (kind === 'audio') {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 rounded-[8px] border border-hairline bg-black/30 p-4">
        <Volume2 size={22} className="text-fuchsia-300/80" />
        <div className="flex h-10 items-end gap-[3px]">
          {Array.from({ length: 28 }).map((_, i) => (
            <span
              key={i}
              className="w-[3px] rounded-sm bg-fuchsia-300/60 transition-all duration-300"
              style={{
                height: `${(playing ? 20 + Math.abs(Math.sin(i * 1.7 + t)) * 80 : 12 + (i % 4) * 6) * 0.3}px`,
                opacity: playing ? 0.5 + Math.abs(Math.sin(i + t)) * 0.5 : 0.25,
              }}
            />
          ))}
        </div>
        <div className="font-mono text-[10px] text-ink-faint">
          {formatClock(t)} / {formatClock(seconds)} · simulated stream
        </div>
        <button
          onClick={() => setPlaying((p) => !p)}
          className="flex items-center gap-2 rounded-full border border-hairline-strong px-4 py-1.5 text-[12px] text-ink-mut transition-colors hover:border-amber/40 hover:text-amber"
        >
          <Play size={11} /> {playing ? 'Pause' : 'Play'}
        </button>
      </div>
    )
  }

  return (
    <div className="flex flex-1 flex-col gap-2">
      <div className="relative flex-1 overflow-hidden rounded-[8px] border border-hairline bg-black/40">
        <img
          src={`https://picsum.photos/seed/${file.id}-v/640/360`}
          alt=""
          draggable={false}
          className="size-full object-cover opacity-80"
        />
        <div className="absolute inset-0 flex items-center justify-center">
          <button
            onClick={() => setPlaying((p) => !p)}
            className="flex size-12 items-center justify-center rounded-full border border-white/20 bg-black/50 text-white backdrop-blur transition-transform hover:scale-105"
          >
            <Play size={16} className="ml-0.5" />
          </button>
        </div>
        <div className="absolute inset-x-3 bottom-2.5">
          <div className="h-[3px] overflow-hidden rounded-full bg-white/15">
            <div
              className="h-full rounded-full bg-amber transition-[width] duration-1000 ease-linear"
              style={{ width: `${(t / seconds) * 100}%` }}
            />
          </div>
          <div className="mt-1 flex justify-between font-mono text-[9.5px] text-white/60">
            <span>{formatClock(t)}</span>
            <span>{formatClock(seconds)} · HTTP-Range stream (simulated)</span>
          </div>
        </div>
      </div>
    </div>
  )
}

export function Preview({ file }: { file: FileInfo }) {
  if (file.kind === 'dir') {
    return (
      <div className="flex flex-1 items-center justify-center rounded-[8px] border border-dashed border-hairline text-[12px] text-ink-faint">
        folder — open it to browse
      </div>
    )
  }
  switch (file.previewKind) {
    case 'image':
      return <ImagePreview file={file} />
    case 'text':
      return <TextPreview file={file} />
    case 'pdf':
      return <PdfPreview file={file} />
    case 'video':
      return <MediaPreview file={file} kind="video" />
    case 'audio':
      return <MediaPreview file={file} kind="audio" />
    default:
      return (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 rounded-[8px] border border-dashed border-hairline text-ink-faint">
          <FileText size={20} />
          <span className="text-[12px]">No inline preview</span>
          <span className="font-mono text-[10px]">download to open · {file.mime}</span>
        </div>
      )
  }
}
