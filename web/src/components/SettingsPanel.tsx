import { useEffect, useState } from 'react'
import { RotateCcw, Save, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import { Card } from './ui/card'
import { EmptyNote, ErrorText } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { apiClient, type LoggingConfig } from '@/lib/api'
import { useAppPreference } from '@/lib/storage'
import type { DialogsApi } from './dialogs'

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

export function SettingsPanel(p: { dialogs: DialogsApi }) {
  const [cfg, setCfg] = useState<LoggingConfig | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [persistHistory, setPersistHistory] = useAppPreference<boolean>('privacy.persistHistory', true)
  const [persistSnapshots, setPersistSnapshots] = useAppPreference<boolean>('privacy.persistSnapshots', false)
  const [retentionDays, setRetentionDays] = useAppPreference<number>('privacy.retentionDays', 30)

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
        <div className="mb-2 text-[12px] font-semibold">Privacy</div>
        <div className="flex flex-col gap-2 text-[12px]">
          <label className="flex items-center justify-between gap-2">
            <span>Persist query history</span>
            <Switch checked={persistHistory} onCheckedChange={setPersistHistory} />
          </label>
          <label className="flex items-center justify-between gap-2">
            <span>Restore result snapshots</span>
            <Switch checked={persistSnapshots} onCheckedChange={setPersistSnapshots} />
          </label>
          <p className="-mt-1 text-[11px] text-muted-foreground">Keep up to 50 result rows per tab across reloads.</p>
          <label className="flex items-center justify-between gap-2">
            <span>History retention</span>
            <span className="flex items-center gap-1">
              <Input
                type="number"
                className="w-[80px]"
                value={retentionDays}
                onChange={(e) => setRetentionDays(Math.min(365, Math.max(1, Number(e.target.value) || 30)))}
              />
              <span className="text-muted-foreground">days</span>
            </span>
          </label>
          <div className="flex gap-1.5">
            <Button
              size="sm"
              variant="secondary"
              className="flex-1"
              onClick={async () => {
                const j = await apiClient.clearHistory()
                if (j.error) toast.error(j.error)
                else toast.success('Query history cleared')
              }}
            >
              <Trash2 /> Clear history
            </Button>
            <Button
              size="sm"
              variant="secondary"
              className="flex-1"
              onClick={async () => {
                const ok = await p.dialogs.confirm({
                  title: 'Clear all local data?',
                  description: 'Removes saved tabs, snapshots, preferences and server history. Sessions stay connected.',
                  confirmText: 'Clear everything',
                  danger: true,
                })
                if (!ok) return
                await apiClient.clearHistory().catch(() => undefined)
                try {
                  localStorage.clear()
                } catch {
                  /* best-effort */
                }
                toast.success('Local data cleared — reloading')
                setTimeout(() => window.location.reload(), 600)
              }}
            >
              <Trash2 /> Clear all local data
            </Button>
          </div>
        </div>
      </Card>
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
            <p className="-mt-1 text-[11px] text-muted-foreground">Minimum severity kept in the log buffer.</p>
            <label className="flex items-center justify-between gap-2">
              <span>HTTP requests</span>
              <Switch checked={cfg.log_http} onCheckedChange={(v) => setCfg({ ...cfg, log_http: v })} />
            </label>
            <p className="-mt-1 text-[11px] text-muted-foreground">Log every API request with status and duration.</p>
            <label className="flex items-center justify-between gap-2">
              <span>Queries</span>
              <Switch checked={cfg.log_query} onCheckedChange={(v) => setCfg({ ...cfg, log_query: v })} />
            </label>
            <p className="-mt-1 text-[11px] text-muted-foreground">Log SQL text, session, and row counts.</p>
            <label className="flex items-center justify-between gap-2">
              <span>Slow query threshold</span>
              <span className="flex items-center gap-1">
                <Input
                  type="number"
                  className="w-[80px]"
                  value={cfg.slow_ms}
                  onChange={(e) => setCfg({ ...cfg, slow_ms: Number(e.target.value) })}
                />
                <span className="text-muted-foreground">ms</span>
              </span>
            </label>
            <p className="-mt-1 text-[11px] text-muted-foreground">Queries taking longer than this are logged as warnings.</p>
            <label className="flex items-center justify-between gap-2">
              <span>Max entries</span>
              <Input
                type="number"
                className="w-[110px]"
                value={cfg.max_entries}
                onChange={(e) => setCfg({ ...cfg, max_entries: Number(e.target.value) })}
              />
            </label>
            <p className="-mt-1 text-[11px] text-muted-foreground">Maximum number of log entries kept in memory. Steady-state queries log at debug; slow queries warn, failures error.</p>
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
