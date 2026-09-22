import * as React from 'react'
import { cn } from '@/lib/utils'

type TableProps = React.HTMLAttributes<HTMLTableElement> & {
  /** Optional class for the scroll container, used by virtualized tables. */
  containerClassName?: string
  containerRef?: React.Ref<HTMLDivElement>
}

const Table = React.forwardRef<HTMLTableElement, TableProps>(({ className, containerClassName, containerRef, ...props }, ref) => (
  <div ref={containerRef} data-pglight-scroll-container className={cn('relative min-w-0 overflow-auto rounded-md border', containerClassName)}>
    <table ref={ref} className={cn('w-full caption-bottom text-xs', className)} {...props} />
  </div>
))
Table.displayName = 'Table'

const TableHeader = React.forwardRef<HTMLTableSectionElement, React.HTMLAttributes<HTMLTableSectionElement>>(({ className, ...props }, ref) => (
  <thead ref={ref} className={cn('[&_tr]:border-b', className)} {...props} />
))
TableHeader.displayName = 'TableHeader'

const TableBody = React.forwardRef<HTMLTableSectionElement, React.HTMLAttributes<HTMLTableSectionElement>>(({ className, ...props }, ref) => (
  <tbody ref={ref} className={cn('[&_tr:last-child]:border-0', className)} {...props} />
))
TableBody.displayName = 'TableBody'

const TableRow = React.forwardRef<HTMLTableRowElement, React.HTMLAttributes<HTMLTableRowElement>>(({ className, ...props }, ref) => (
  <tr ref={ref} className={cn('border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted', className)} {...props} />
))
TableRow.displayName = 'TableRow'

const TableHead = React.forwardRef<HTMLTableCellElement, React.ThHTMLAttributes<HTMLTableCellElement>>(({ className, ...props }, ref) => (
  <th ref={ref} className={cn('sticky top-0 z-10 h-8 bg-card px-2.5 text-left align-middle font-medium text-muted-foreground whitespace-nowrap max-w-[320px] overflow-hidden text-ellipsis', className)} {...props} />
))
TableHead.displayName = 'TableHead'

const TableCell = React.forwardRef<HTMLTableCellElement, React.TdHTMLAttributes<HTMLTableCellElement>>(({ className, ...props }, ref) => (
  <td ref={ref} className={cn('px-2.5 py-1.5 align-middle whitespace-nowrap max-w-[320px] overflow-hidden text-ellipsis', className)} {...props} />
))
TableCell.displayName = 'TableCell'

export { Table, TableHeader, TableBody, TableRow, TableHead, TableCell }
