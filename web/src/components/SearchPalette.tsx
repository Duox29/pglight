import { useEffect, useRef, useState } from 'react'
import { Search } from 'lucide-react'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { EmptyNote } from './ui/feedback'
import { api, q } from '@/lib/api'

interface Hit {
  kind: string
  schema: string
  name: string
  detail: string
}

export function SearchPalette(props: {
  open: boolean
  onOpenChange: (v: boolean) => void
  session: string
  onOpenTable: (schema: string, table: string) => void
}) {
  const [term, setTerm] = useState('')
  const [hits, setHits] = useState<Hit[]>([])
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const seq = useRef(0)

  useEffect(() => () => clearTimeout(timer.current), [])

  const onChange = (v: string) => {
    setTerm(v)
    clearTimeout(timer.current)
    if (v.trim().length < 2 || !props.session) {
      setHits([])
      return
    }
    const n = ++seq.current
    const query = v.trim()
    const sid = props.session
    timer.current = setTimeout(async () => {
      try {
        const j = await api<Hit[] | { error?: string }>(q(sid, `/api/search?q=${encodeURIComponent(query)}`))
        if (seq.current !== n) return
        setHits(Array.isArray(j) ? j.filter((h) => h.kind !== 'function') : [])
      } catch {
        if (seq.current !== n) return
        setHits([])
      }
    }, 200)
  }

  const go = (r: Hit) => {
    props.onOpenChange(false)
    if (r.kind === 'table' || r.kind === 'column') props.onOpenTable(r.schema, r.name.split('.')[0])
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Search className="h-4 w-4" /> Search objects
          </DialogTitle>
        </DialogHeader>
        <Input
          autoFocus
          placeholder="Type to search tables, columns…"
          value={term}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && hits[0]) go(hits[0])
          }}
        />
        <div className="max-h-[50vh] overflow-auto">
          {hits.map((r, i) => (
            <div key={i} className="cursor-pointer rounded border-b p-1.5 hover:bg-accent" onClick={() => go(r)}>
              <span className="text-[11px] text-muted-foreground">{r.kind}</span>{' '}
              <b className="text-[12px]">
                {r.schema}.{r.name}
              </b>{' '}
              <span className="text-[11px] text-muted-foreground">{r.detail}</span>
            </div>
          ))}
          {term.trim().length >= 2 && !hits.length && <EmptyNote text="No matches" />}
        </div>
      </DialogContent>
    </Dialog>
  )
}
