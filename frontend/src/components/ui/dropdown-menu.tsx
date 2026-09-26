import * as DD from '@radix-ui/react-dropdown-menu'
import { forwardRef } from 'react'
import { Check, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'

export const DropdownMenu = DD.Root
export const DropdownMenuTrigger = DD.Trigger

export const DropdownMenuContent = forwardRef<
  React.ComponentRef<typeof DD.Content>,
  React.ComponentPropsWithoutRef<typeof DD.Content>
>(({ className, sideOffset = 6, ...props }, ref) => (
  <DD.Portal>
    <DD.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn(
        'z-50 min-w-[196px] overflow-hidden rounded-[8px] border border-hairline-strong bg-overlay p-1',
        'shadow-[0_16px_48px_-8px_rgba(0,0,0,0.75)] data-[state=open]:animate-pop-in',
        className,
      )}
      {...props}
    />
  </DD.Portal>
))
DropdownMenuContent.displayName = 'DropdownMenuContent'

export const DropdownMenuItem = forwardRef<
  React.ComponentRef<typeof DD.Item>,
  React.ComponentPropsWithoutRef<typeof DD.Item> & { danger?: boolean }
>(({ className, danger, ...props }, ref) => (
  <DD.Item
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
DropdownMenuItem.displayName = 'DropdownMenuItem'

export const DropdownMenuCheckboxItem = forwardRef<
  React.ComponentRef<typeof DD.CheckboxItem>,
  React.ComponentPropsWithoutRef<typeof DD.CheckboxItem>
>(({ className, children, ...props }, ref) => (
  <DD.CheckboxItem
    ref={ref}
    className={cn(
      'flex h-[30px] cursor-default select-none items-center gap-2 rounded-[5px] px-2.5 text-[13px] text-ink-mut outline-none',
      'data-[highlighted]:bg-amber/12 data-[highlighted]:text-ink',
      className,
    )}
    {...props}
  >
    <span className="flex size-3.5 items-center justify-center">
      <DD.ItemIndicator>
        <Check size={13} className="text-amber" />
      </DD.ItemIndicator>
    </span>
    {children}
  </DD.CheckboxItem>
))
DropdownMenuCheckboxItem.displayName = 'DropdownMenuCheckboxItem'

export const DropdownMenuRadioGroup = DD.RadioGroup

export const DropdownMenuRadioItem = forwardRef<
  React.ComponentRef<typeof DD.RadioItem>,
  React.ComponentPropsWithoutRef<typeof DD.RadioItem>
>(({ className, children, ...props }, ref) => (
  <DD.RadioItem
    ref={ref}
    className={cn(
      'flex h-[30px] cursor-default select-none items-center gap-2 rounded-[5px] px-2.5 text-[13px] text-ink-mut outline-none',
      'data-[highlighted]:bg-amber/12 data-[highlighted]:text-ink',
      className,
    )}
    {...props}
  >
    <span className="flex size-3.5 items-center justify-center">
      <DD.ItemIndicator>
        <Check size={13} className="text-amber" />
      </DD.ItemIndicator>
    </span>
    {children}
  </DD.RadioItem>
))
DropdownMenuRadioItem.displayName = 'DropdownMenuRadioItem'

export const DropdownMenuSeparator = () => <DD.Separator className="my-1 h-px bg-hairline" />

export const DropdownMenuLabel = ({ children }: { children: React.ReactNode }) => (
  <DD.Label className="px-2.5 pt-1.5 pb-1 font-mono text-[10px] uppercase tracking-wider text-ink-faint">
    {children}
  </DD.Label>
)

export const DropdownMenuSub = DD.Sub

export const DropdownMenuSubTrigger = forwardRef<
  React.ComponentRef<typeof DD.SubTrigger>,
  React.ComponentPropsWithoutRef<typeof DD.SubTrigger>
>(({ className, children, ...props }, ref) => (
  <DD.SubTrigger
    ref={ref}
    className={cn(
      'flex h-[30px] cursor-default select-none items-center gap-2.5 rounded-[5px] px-2.5 text-[13px] text-ink-mut outline-none',
      'data-[highlighted]:bg-amber/12 data-[highlighted]:text-ink data-[state=open]:bg-amber/12 [&_svg]:size-[15px]',
      className,
    )}
    {...props}
  >
    {children}
    <ChevronRight size={13} className="ml-auto" />
  </DD.SubTrigger>
))
DropdownMenuSubTrigger.displayName = 'DropdownMenuSubTrigger'

export const DropdownMenuSubContent = forwardRef<
  React.ComponentRef<typeof DD.SubContent>,
  React.ComponentPropsWithoutRef<typeof DD.SubContent>
>(({ className, ...props }, ref) => (
  <DD.Portal>
    <DD.SubContent
      ref={ref}
      className={cn(
        'z-50 min-w-[176px] overflow-hidden rounded-[8px] border border-hairline-strong bg-overlay p-1',
        'shadow-[0_16px_48px_-8px_rgba(0,0,0,0.75)] data-[state=open]:animate-pop-in',
        className,
      )}
      {...props}
    />
  </DD.Portal>
))
DropdownMenuSubContent.displayName = 'DropdownMenuSubContent'
