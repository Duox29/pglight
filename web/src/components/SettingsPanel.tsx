import { useEffect, useState } from 'react'
import { Save, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import { Card } from './ui/card'
import { DataGrid } from './ui/data-grid'
import { EmptyNote, ErrorText } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { apiClient, type LogEntry, type LoggingConfig } from '@/lib/api'

const LEVELS = ['debug', 'info', 'warn', 'error']
const CATEGORIES = ['http', 'query', 'txn', 'system']

export function SettingsPanel() {
  const [cfg, setCfg] = useState<LoggingConfig | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [levelFilter, setLevelFilter] = useState('')
  const [catFilter, setCatFilter] = useState('')
  const [auto, setAuto] = useState(true)
  const [entries, setEntries] = useState<LogEntry[]>([])

  useEffect(() => {
    let stop = false
    const load = async () => {
      try {
        const j = await apiClient.getSettings()
        if (!stop) {
          setCfg(j.logging)
          setError('')
        }
      } catch (e) {
        if (!stop) setError(String(e))
      }
    }
    load()
    return () => {
      stop = true
    }
  }, [])

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

  const save = async () => {
    if (!cfg) return
    setSaving(true)
    try {
      const j = await apiClient.saveSettings(cfg)
      if (j.error) toast.error(j.error)
      else {
        setCfg(j.logging)
        toast.success('Settings saved')
      }
    } finally {
      setSaving(false)
    }
  }

  const clear = async () => {
    const j = await apiClient.clearLogs()
    if (j.error) toast.error(j.error)
    else {
      setEntries([])
      toast.success('Logs cleared')
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <ErrorText message={error} />
      <Card className="p-2.5">
        <div className="mb-2 text-[12px] font-semibold">Logging (AOP)</div>
        {!cfg ? (
          <EmptyNote text="Loading…" />
        ) : (
          <div className="flex flex-col gap-2 text-[12px]">
            <label className="flex items-center justify-between gap-2">
              <span>Enabled</span>
              <Switch checked={cfg.enabled} onCheckedChange={(v) => setCfg({ ...cfg, enabled: v })} />
            </label>
            <label className="flex items-center justify-between gap-2">
              <span>Level</span>
              <Select value={cfg.level} onValueChange={(v) => setCfg({ ...cfg, level: v })}>
                <SelectTrigger className="w-[110px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {LEVELS.map((l) => (
                    <SelectItem key={l} value={l}>
                      {l}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
            <label className="flex items-center justify-between gap-2">
              <span>HTTP requests</span>
              <Switch checked={cfg.log_http} onCheckedChange={(v) => setCfg({ ...cfg, log_http: v })} />
            </label>
            <label className="flex items-center justify-between gap-2">
              <span>Queries</span>
              <Switch checked={cfg.log_query} onCheckedChange={(v) => setCfg({ ...cfg, log_query: v })} />
            </label>
            <label className="flex items-center justify-between gap-2">
              <span>Slow threshold (ms)</span>
              <Input
                type="number"
                className="w-[110px]"
                value={cfg.slow_ms}
                onChange={(e) => setCfg({ ...cfg, slow_ms: Number(e.target.value) })}
              />
            </label>
            <label className="flex items-center justify-between gap-2">
              <span>Max entries</span>
              <Input
                type="number"
                className="w-[110px]"
                value={cfg.max_entries}
                onChange={(e) => setCfg({ ...cfg, max_entries: Number(e.target.value) })}
              />
            </label>
            <div className="text-[11px] text-muted-foreground">
              Steady-state queries log at debug; slow queries warn, failures error.
            </div>
            <Button size="sm" onClick={save} disabled={saving}>
              <Save /> Save
            </Button>
          </div>
        )}
      </Card>
      <Card className="p-2.5">
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
          />
        ) : (
          <EmptyNote text="No entries — run a query or lower the level to debug" />
        )}
      </Card>
    </div>
  )
}
