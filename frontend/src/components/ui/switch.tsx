import * as SwitchPrimitive from '@radix-ui/react-switch'
import { forwardRef } from 'react'
import { cn } from '@/lib/utils'

export const Switch = forwardRef<
  React.ComponentRef<typeof SwitchPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof SwitchPrimitive.Root>
>(({ className, ...props }, ref) => (
  <SwitchPrimitive.Root
    ref={ref}
    className={cn(
      'peer inline-flex h-[18px] w-[32px] shrink-0 cursor-pointer items-center rounded-full border border-hairline-strong transition-colors',
      'data-[state=checked]:border-amber/50 data-[state=checked]:bg-amber data-[state=unchecked]:bg-black/40',
      className,
    )}
    {...props}
  >
    <SwitchPrimitive.Thumb
      className={cn(
        'pointer-events-none block size-[12px] rounded-full bg-ink-mut shadow transition-transform',
        'data-[state=checked]:translate-x-[16px] data-[state=checked]:bg-on-amber data-[state=unchecked]:translate-x-[3px]',
      )}
    />
  </SwitchPrimitive.Root>
))
Switch.displayName = 'Switch'
