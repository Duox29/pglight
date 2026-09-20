import { useEffect, useState, type KeyboardEvent } from 'react'
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
import { COMMANDS } from '@/commands/registry'
import type { CommandId } from '@/commands/types'
import { useCommand } from '@/shortcuts/ShortcutProvider'
import { isReservedShortcut } from '@/shortcuts/reserved'
import { normalizeShortcut, shortcutForDisplay, shortcutFromEvent } from '@/shortcuts/normalize'

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
  const commands = useCommand()
  const [cfg, setCfg] = useState<LoggingConfig | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [persistHistory, setPersistHistory] = useAppPreference<boolean>('privacy.persistHistory', true)
  const [persistSnapshots, setPersistSnapshots] = useAppPreference<boolean>('privacy.persistSnapshots', false)
  const [retentionDays, setRetentionDays] = useAppPreference<number>('privacy.retentionDays', 30)
  const [shortcutSearch, setShortcutSearch] = useState('')
  const [recording, setRecording] = useState<CommandId | null>(null)

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

  const assignShortcut = async (id: CommandId, event: KeyboardEvent<HTMLButtonElement>) => {
    event.preventDefault()
    event.stopPropagation()
    if (event.key === 'Escape') {
      setRecording(null)
      return
    }
    const binding = shortcutFromEvent(event.nativeEvent)
    if (!binding) return
    const normalized = normalizeShortcut(binding)
    if (!normalized) return
    const conflict = commands.conflictFor(normalized, id)
    if (conflict) {
      const replace = await p.dialogs.confirm({
        title: 'Shortcut already assigned',
        description: `${shortcutForDisplay(normalized)} is already assigned to ${COMMANDS.find((command) => command.id === conflict)?.title ?? conflict}. Replace it?`,
        confirmText: 'Replace',
      })
      if (!replace) return
      commands.setBindings(conflict, commands.formatBinding(conflict).filter((value) => value !== normalized))
    }
    commands.setBindings(id, [normalized])
    setRecording(null)
  }

  return (
    <div className="flex flex-col gap-2">
      <ErrorText message={error} />
      <Card className="p-2.5">
        <div className="mb-2 text-[12px] font-semibold">Privacy</div>
        <div className="flex flex-col gap-2 text-[12px]">
          <p className="text-[11px] text-muted-foreground">
            Query text, snippets and history live in a private app database (0700/0600). Result rows are the most
            sensitive part — they restore only when enabled below.
          </p>
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
        <div className="mb-2 text-[12px] font-semibold">Keyboard Shortcuts</div>
        <Input value={shortcutSearch} onChange={(event) => setShortcutSearch(event.target.value)} placeholder="Search commands…" className="mb-2" />
        <div className="max-h-[340px] overflow-auto">
          {COMMANDS.filter((command) => `${command.title} ${command.id} ${command.category}`.toLowerCase().includes(shortcutSearch.toLowerCase())).map((command) => {
            const bindings = commands.formatBinding(command.id)
            const binding = bindings[0]
            return (
              <div key={command.id} className="flex items-center gap-2 border-b py-1.5 last:border-0">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[12px]">{command.title}</div>
                  <div className="text-[10px] text-muted-foreground">{command.category}</div>
                </div>
                {recording === command.id ? (
                  <Button size="sm" variant="secondary" autoFocus onKeyDown={(event) => void assignShortcut(command.id, event)}>Press keys…</Button>
                ) : (
                  <Button size="sm" variant="outline" onClick={() => setRecording(command.id)}>
                    {binding ? shortcutForDisplay(binding) : 'Unassigned'}
                  </Button>
                )}
                {binding && isReservedShortcut(binding) && <span className="text-[10px] text-amber-500">Browser reserved</span>}
                {commands.formatBinding(command.id).length > 0 && <Button size="sm" variant="ghost" onClick={() => commands.resetBinding(command.id)} aria-label={`Reset ${command.title}`}>Reset</Button>}
              </div>
            )
          })}
        </div>
        <div className="mt-2 flex items-center justify-between gap-2 text-[10px] text-muted-foreground">
          <span>Overrides sync to the server. Browser-reserved shortcuts may not reach pglight.</span>
          <Button size="sm" variant="secondary" onClick={async () => {
            const ok = await p.dialogs.confirm({ title: 'Reset keyboard shortcuts?', description: 'Restore every command to its default binding.', confirmText: 'Reset all' })
            if (ok) commands.resetAll()
          }}>Reset all</Button>
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
