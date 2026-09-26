import { forwardRef } from 'react'
import { cn } from '@/lib/utils'

export const Input = forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        'h-8 w-full rounded-[6px] bg-black/30 border border-hairline-strong px-2.5 text-[13px] text-ink placeholder:text-ink-faint',
        'transition-colors focus:border-amber/60 focus:bg-black/40',
        className,
      )}
      {...props}
    />
  ),
)
Input.displayName = 'Input'
