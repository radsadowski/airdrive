import { CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { useUiStore } from '@/stores/ui'
import { cn } from '@/lib/utils'

export function Toasts() {
  const toasts = useUiStore((s) => s.toasts)
  const dismiss = useUiStore((s) => s.dismissToast)

  return (
    <div className="pointer-events-none fixed bottom-5 left-1/2 z-[70] flex w-[380px] -translate-x-1/2 flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={cn(
            'pointer-events-auto flex items-start gap-2.5 rounded-[8px] border bg-overlay/95 px-3 py-2.5 shadow-[0_12px_40px_-6px_rgba(0,0,0,0.8)] backdrop-blur animate-rise',
            t.variant === 'success' && 'border-success/30',
            t.variant === 'error' && 'border-danger/40',
            (!t.variant || t.variant === 'default') && 'border-hairline-strong',
          )}
        >
          {t.variant === 'success' ? (
            <CheckCircle2 size={15} className="mt-0.5 shrink-0 text-success" />
          ) : t.variant === 'error' ? (
            <XCircle size={15} className="mt-0.5 shrink-0 text-danger" />
          ) : (
            <Info size={15} className="mt-0.5 shrink-0 text-amber" />
          )}
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-medium leading-snug text-ink">{t.title}</div>
            {t.description && <div className="mt-0.5 text-[12px] leading-snug text-ink-mut">{t.description}</div>}
          </div>
          {t.actionLabel && (
            <button
              className="shrink-0 self-center text-[12px] font-medium text-amber hover:text-amber-hi"
              onClick={() => dismiss(t.id)}
            >
              {t.actionLabel}
            </button>
          )}
          <button
            className="shrink-0 self-center text-ink-faint transition-colors hover:text-ink"
            onClick={() => dismiss(t.id)}
          >
            <X size={13} />
          </button>
        </div>
      ))}
    </div>
  )
}
