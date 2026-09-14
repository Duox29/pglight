import * as React from 'react'
import * as TooltipPrimitive from '@radix-ui/react-tooltip'
import { cn } from '@/lib/utils'

function TooltipProvider(props: React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Provider>) {
  // Global: moving the pointer onto the tooltip content MUST NOT keep it open —
  // tooltips close as soon as the pointer leaves the trigger.
  return <TooltipPrimitive.Provider {...props} disableHoverableContent />
}
const Tooltip = TooltipPrimitive.Root
const TooltipTrigger = TooltipPrimitive.Trigger

const TooltipContent = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>
>(({ className, sideOffset = 4, style, ...props }, ref) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn('z-50 max-h-[300px] max-w-[420px] overflow-auto break-words rounded-md border border-border bg-popover px-2.5 py-1.5 text-[12px] leading-relaxed text-popover-foreground shadow-lg', className)}
      style={{ ...style, pointerEvents: 'none' }}
      {...props}
    />
  </TooltipPrimitive.Portal>
))
TooltipContent.displayName = TooltipPrimitive.Content.displayName

function Tip({
  content,
  children,
  side,
  align,
  className,
}: {
  content: React.ReactNode
  children: React.ReactNode
  side?: React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>['side']
  align?: React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>['align']
  className?: string
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side={side} align={align} className={className}>
        {content}
      </TooltipContent>
    </Tooltip>
  )
}

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider, Tip }
