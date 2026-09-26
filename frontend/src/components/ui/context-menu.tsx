import * as CM from '@radix-ui/react-context-menu'
import { forwardRef } from 'react'
import { cn } from '@/lib/utils'

export const ContextMenu = CM.Root
export const ContextMenuTrigger = CM.Trigger

export const ContextMenuContent = forwardRef<
  React.ComponentRef<typeof CM.Content>,
  React.ComponentPropsWithoutRef<typeof CM.Content>
>(({ className, ...props }, ref) => (
  <CM.Portal>
    <CM.Content
      ref={ref}
      className={cn(
        'z-50 min-w-[196px] overflow-hidden rounded-[8px] border border-hairline-strong bg-overlay p-1',
        'shadow-[0_16px_48px_-8px_rgba(0,0,0,0.75)] data-[state=open]:animate-pop-in',
        className,
      )}
      {...props}
    />
  </CM.Portal>
))
ContextMenuContent.displayName = 'ContextMenuContent'

export const ContextMenuItem = forwardRef<
  React.ComponentRef<typeof CM.Item>,
  React.ComponentPropsWithoutRef<typeof CM.Item> & { danger?: boolean }
>(({ className, danger, ...props }, ref) => (
  <CM.Item
    ref={ref}
    className={cn(
      'flex h-[30px] cursor-default select-none items-center gap-2.5 rounded-[5px] px-2.5 text-[13px] text-ink-mut outline-none',
      'transition-colors data-[highlighted]:bg-amber/12 data-[highlighted]:text-ink [&_svg]:size-[15px] [&_svg]:shrink-0',
      danger && 'data-[highlighted]:bg-danger/15 data-[highlighted]:text-danger',
      className,
    )}
    {...props}
  />
))
ContextMenuItem.displayName = 'ContextMenuItem'

export const ContextMenuSeparator = () => (
  <CM.Separator className="my-1 h-px bg-hairline" />
)

export const ContextMenuLabel = ({ children }: { children: React.ReactNode }) => (
  <CM.Label className="px-2.5 pt-1.5 pb-1 font-mono text-[10px] uppercase tracking-wider text-ink-faint">
    {children}
  </CM.Label>
)
