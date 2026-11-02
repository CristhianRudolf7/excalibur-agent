import type { ComponentProps } from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { Slot } from 'radix-ui'
import { cn } from '@/lib/utils'

// shadcn/ui button, adapted to the application's semantic control styles.
const buttonVariants = cva('button', {
  variants: {
    variant: {
      default: '',
      secondary: 'secondary-button',
      destructive: 'destructive-button',
      primary: 'primary-button',
      ghost: 'ghost-button',
      icon: 'icon-button',
    },
  },
  defaultVariants: { variant: 'default' },
})
export function Button({
  className,
  variant,
  asChild = false,
  type = 'button',
  ...props
}: ComponentProps<'button'> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Component = asChild ? Slot.Root : 'button'
  return (
    <Component
      type={asChild ? undefined : type}
      data-slot="button"
      className={cn(buttonVariants({ variant, className }))}
      {...props}
    />
  )
}
