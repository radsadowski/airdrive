import { cva, type VariantProps } from 'class-variance-authority'
import { forwardRef } from 'react'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-[6px] text-[13px] font-medium transition-all duration-100 select-none disabled:pointer-events-none disabled:opacity-40 active:scale-[0.98] [&_svg]:pointer-events-none [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        primary: 'bg-amber text-on-amber hover:bg-amber-hi shadow-[0_1px_0_rgba(255,183,51,0.35)_inset,0_0_0_1px_rgba(0,0,0,0.4)] font-semibold',
        secondary: 'bg-raised text-ink border border-hairline-strong hover:bg-overlay hover:border-amber/40',
        ghost: 'text-ink-mut hover:text-ink hover:bg-white/5',
        danger: 'bg-danger/15 text-danger border border-danger/30 hover:bg-danger/25',
        link: 'text-amber hover:text-amber-hi underline-offset-2 hover:underline',
      },
      size: {
        sm: 'h-7 px-2.5 [&_svg]:size-3.5',
        md: 'h-8 px-3.5 [&_svg]:size-4',
        lg: 'h-9 px-5 text-sm',
        icon: 'h-7 w-7 [&_svg]:size-4',
        'icon-lg': 'h-9 w-9 [&_svg]:size-[18px]',
      },
    },
    defaultVariants: { variant: 'secondary', size: 'md' },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, ...props }, ref) => (
    <button ref={ref} className={cn(buttonVariants({ variant, size }), className)} {...props} />
  ),
)
Button.displayName = 'Button'
