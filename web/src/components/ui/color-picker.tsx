import { Input } from './input'
import { cn } from '@/lib/utils'

interface ColorPickerProps {
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  className?: string
}

export function ColorPicker({ value, onChange, ariaLabel, className }: ColorPickerProps) {
  return (
    <div className={cn('flex items-center gap-1.5', className)}>
      <Input
        type="color"
        value={value}
        aria-label={ariaLabel}
        className="h-8 w-9 cursor-pointer p-1"
        onChange={(event) => onChange(event.target.value)}
      />
      <span className="w-[68px] font-mono text-[11px] uppercase text-muted-foreground">{value}</span>
    </div>
  )
}
