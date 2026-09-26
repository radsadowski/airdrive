import { forwardRef } from 'react'
import { cn } from '@/lib/utils'

export const Progress = forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement> & { value: number; indeterminate?: boolean; tone?: 'amber' | 'success' | 'danger' }
>(({ className, value, indeterminate, tone = 'amber', ...props }, ref) => (
  <div
    ref={ref}
    className={cn('relative h-[3px] w-full overflow-hidden rounded-full bg-white/[0.07]', className)}
    {...props}
  >
    {indeterminate ? (
      <div
        className="h-full w-1/2 rounded-full bg-amber animate-shimmer"
        style={{ background: 'linear-gradient(90deg, transparent, var(--color-amber), transparent)', backgroundSize: '200% 100%' }}
      />
    ) : (
      <div
        className={cn(
          'h-full rounded-full transition-[width] duration-300 ease-out',
          tone === 'amber' && 'bg-amber',
          tone === 'success' && 'bg-success',
          tone === 'danger' && 'bg-danger',
        )}
        style={{ width: `${Math.min(100, Math.max(0, value * 100))}%` }}
      />
    )}
  </div>
))
Progress.displayName = 'Progress'
