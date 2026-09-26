import {
  Archive,
  FileAudio,
  FileCode2,
  FileImage,
  FileText,
  FileVideo,
  File as FileLucide,
  Folder,
  type LucideIcon,
} from 'lucide-react'
import type { FileInfo } from '@/lib/api/contract'
import { fileMeta } from '@/lib/api/mock/data'
import { cn } from '@/lib/utils'

const CATEGORY_STYLE: Record<string, { icon: LucideIcon; fg: string; bg: string }> = {
  image: { icon: FileImage, fg: 'text-sky-300', bg: 'bg-sky-400/10' },
  video: { icon: FileVideo, fg: 'text-rose-300', bg: 'bg-rose-400/10' },
  audio: { icon: FileAudio, fg: 'text-fuchsia-300', bg: 'bg-fuchsia-400/10' },
  pdf: { icon: FileText, fg: 'text-red-300', bg: 'bg-red-400/10' },
  document: { icon: FileText, fg: 'text-amber-200/90', bg: 'bg-amber-300/10' },
  code: { icon: FileCode2, fg: 'text-emerald-300', bg: 'bg-emerald-400/10' },
  archive: { icon: Archive, fg: 'text-orange-300', bg: 'bg-orange-400/10' },
  other: { icon: FileLucide, fg: 'text-ink-mut', bg: 'bg-white/[0.05]' },
}

export function FileIcon({ file, size = 16, className }: { file: FileInfo; size?: number; className?: string }) {
  if (file.kind === 'dir') {
    return <Folder size={size} className={cn('shrink-0 text-amber/80', className)} strokeWidth={1.6} />
  }
  const { category } = fileMeta(file.name)
  const style = CATEGORY_STYLE[category] ?? CATEGORY_STYLE.other
  const Icon = style.icon
  return <Icon size={size} className={cn('shrink-0', style.fg, className)} strokeWidth={1.6} />
}

/** colored tile variant for grid view */
export function FileIconTile({ file, size = 40 }: { file: FileInfo; size?: number }) {
  if (file.kind === 'dir') {
    return (
      <div className="flex items-center justify-center" style={{ width: size, height: size }}>
        <Folder size={size * 0.7} className="text-amber/80" strokeWidth={1.3} />
      </div>
    )
  }
  const { category } = fileMeta(file.name)
  const style = CATEGORY_STYLE[category] ?? CATEGORY_STYLE.other
  const Icon = style.icon
  return (
    <div
      className={cn('flex items-center justify-center rounded-[8px] border border-hairline', style.bg)}
      style={{ width: size, height: size }}
    >
      <Icon size={size * 0.5} className={style.fg} strokeWidth={1.4} />
    </div>
  )
}
