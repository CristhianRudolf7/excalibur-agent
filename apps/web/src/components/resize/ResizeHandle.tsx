import { cn } from '@/lib/utils'
import { useResizableSize, type ResizableSizeOptions } from './useResizableSize'

export function ResizeHandle({
  label,
  className,
  ...options
}: ResizableSizeOptions & { label: string; className?: string }) {
  const { dragging, ...handlers } = useResizableSize(options)
  return (
    <div
      role="separator"
      tabIndex={0}
      aria-orientation="vertical"
      aria-label={label}
      aria-valuemin={options.min}
      aria-valuemax={options.max}
      aria-valuenow={Math.round(options.value)}
      className={cn('resize-handle', className, dragging && 'dragging')}
      {...handlers}
    />
  )
}
