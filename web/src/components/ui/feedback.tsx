import { cn } from '@/lib/utils'

export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('animate-pulse rounded-md bg-muted', className)} {...props} />
}

export function ErrorText({ message }: { message?: string }) {
  if (!message) return null
  return <div className="whitespace-pre-wrap text-[12px] text-red-400">{message}</div>
}

export function EmptyNote({ text }: { text: string }) {
  return <div className="py-6 text-center text-muted-foreground">{text}</div>
}
