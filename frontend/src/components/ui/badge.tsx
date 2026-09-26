import { forwardRef } from 'react'
import { cn } from '@/lib/utils'

export const Kbd = ({ children, className }: { children: React.ReactNode; className?: string }) => (
  <kbd
    className={cn(
      'inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-[4px] border border-hairline-strong bg-black/40 px-1 font-mono text-[10px] text-ink-mut',
      className,
    )}
  >
    {children}
  </kbd>
)
Kbd.displayName = 'Kbd'

export const Badge = forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement> & { tone?: 'default' | 'amber' | 'success' | 'danger' | 'info' }
>(({ className, tone = 'default', ...props }, ref) => (
  <span
    ref={ref}
    className={cn(
      'inline-flex h-[17px] items-center gap-1 rounded-[4px] border px-1.5 font-mono text-[10px] font-medium uppercase tracking-wide',
      tone === 'default' && 'border-hairline-strong bg-white/[0.04] text-ink-mut',
      tone === 'amber' && 'border-amber/30 bg-amber/10 text-amber',
      tone === 'success' && 'border-success/30 bg-success/10 text-success',
      tone === 'danger' && 'border-danger/30 bg-danger/10 text-danger',
      tone === 'info' && 'border-sky-400/30 bg-sky-400/10 text-sky-300',
      className,
    )}
    {...props}
  />
))
Badge.displayName = 'Badge'
