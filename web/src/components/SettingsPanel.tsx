import { useEffect, useState } from 'react'
import { RotateCcw, Save } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import { Card } from './ui/card'
import { EmptyNote, ErrorText } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { apiClient, type LoggingConfig } from '@/lib/api'

const LEVELS = ['debug', 'info', 'warn', 'error']

// Mirrors logging.DefaultConfig() on the backend.
const DEFAULTS: LoggingConfig = {
  enabled: true,
  level: 'info',
  log_http: true,
  log_query: true,
  slow_ms: 500,
  max_entries: 500,
}

export function SettingsPanel() {
  const [cfg, setCfg] = useState<LoggingConfig | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

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

  const reset = async () => {
    setSaving(true)
    try {
      const j = await apiClient.saveSettings(DEFAULTS)
      if (j.error) toast.error(j.error)
      else {
        setCfg(j.logging)
        toast.success('Reset to defaults')
      }
    } finally {
      setSaving(false)
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
            <div className="flex gap-1.5">
              <Button size="sm" className="flex-1" onClick={save} disabled={saving}>
                <Save /> Save
              </Button>
              <Tip content="Reset to defaults">
                <span className="inline-flex">
                  <Button size="sm" variant="secondary" onClick={reset} disabled={saving}>
                    <RotateCcw /> Defaults
                  </Button>
                </span>
              </Tip>
            </div>
          </div>
        )}
      </Card>
    </div>
  )
}
