import { useEffect, useState } from 'react'
import { Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Switch } from './ui/switch'
import { Card } from './ui/card'
import { DataGrid } from './ui/data-grid'
import { EmptyNote } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { apiClient, type LogEntry } from '@/lib/api'

const LEVELS = ['debug', 'info', 'warn', 'error']
const CATEGORIES = ['http', 'query', 'txn', 'system']

export function LogsPanel() {
  const [levelFilter, setLevelFilter] = useState('')
  const [catFilter, setCatFilter] = useState('')
  const [auto, setAuto] = useState(true)
  const [entries, setEntries] = useState<LogEntry[]>([])

  useEffect(() => {
    let stop = false
    const load = async () => {
      try {
        const j = await apiClient.getLogs({ limit: 200, level: levelFilter || undefined, category: catFilter || undefined })
        if (!stop && !j.error) setEntries(j.entries)
      } catch {
        /* keep old entries on transient failure */
      }
    }
    load()
    if (!auto)
      return () => {
        stop = true
      }
    const t = setInterval(load, 2000)
    return () => {
      stop = true
      clearInterval(t)
    }
  }, [auto, levelFilter, catFilter])

  const clear = async () => {
    const j = await apiClient.clearLogs()
    if (j.error) toast.error(j.error)
    else {
      setEntries([])
      toast.success('Logs cleared')
    }
  }

  return (
    <Card className="flex min-h-0 flex-1 flex-col p-2.5">
      <div className="mb-2 flex items-center gap-1.5">
        <span className="text-[12px] font-semibold">Recent logs</span>
        <span className="flex-1" />
        <Button size="sm" variant="ghost" onClick={clear}>
          <Trash2 /> Clear
        </Button>
      </div>
      <div className="mb-2 flex flex-wrap items-center gap-1.5">
        <Select value={levelFilter || 'all'} onValueChange={(v) => setLevelFilter(v === 'all' ? '' : v)}>
          <SelectTrigger className="w-[100px]">
            <SelectValue placeholder="level" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">all levels</SelectItem>
            {LEVELS.map((l) => (
              <SelectItem key={l} value={l}>
                {l}+
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={catFilter || 'all'} onValueChange={(v) => setCatFilter(v === 'all' ? '' : v)}>
          <SelectTrigger className="w-[110px]">
            <SelectValue placeholder="category" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">all</SelectItem>
            {CATEGORIES.map((c) => (
              <SelectItem key={c} value={c}>
                {c}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <label className="flex items-center gap-1.5 text-[12px] text-muted-foreground">
          <Switch checked={auto} onCheckedChange={setAuto} /> auto
        </label>
      </div>
      {entries.length ? (
        <DataGrid
          data={{
            columns: ['time', 'level', 'cat', 'message', 'ms'],
            rows: entries.map((e) => [
              e.time.slice(11, 19),
              e.level,
              e.category,
              e.detail ? `${e.message} — ${e.detail.slice(0, 120)}` : e.message.slice(0, 160),
              e.duration_ms ?? '',
            ]),
          }}
          containerClassName="min-h-0 max-h-full flex-1"
        />
      ) : (
        <EmptyNote text="No entries — run a query or lower the level to debug" />
      )}
    </Card>
  )
}
