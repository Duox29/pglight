import * as React from 'react'
import { cn } from '@/lib/utils'
import { useState } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { Button } from './button'

const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        'flex min-h-[90px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-[13px] shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    />
  ),
)
Textarea.displayName = 'Textarea'

export function PasswordTextarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  const [visible, setVisible] = useState(false)
  return <div className="relative">
    <Textarea className={cn('pr-9', !visible && '[-webkit-text-security:disc]', className)} {...props} />
    <Button type="button" size="icon" variant="ghost" className="absolute right-1 top-1 h-7 w-7" aria-label={visible ? 'Hide secret text' : 'Show secret text'} onClick={() => setVisible((value) => !value)}>
      {visible ? <EyeOff /> : <Eye />}
    </Button>
  </div>
}

export { Textarea }
