import { useState } from 'react'
import { Plug, PlugZap, RefreshCw, Save, Trash2, KeyRound, ChevronsUpDown, X } from 'lucide-react'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import { Popover, PopoverContent, PopoverTrigger } from './ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import type { SessionInfo } from '@/types'
import type { ConnFields } from './ConnectionBar'

interface Props {
  fields: ConnFields
  setFields: (f: ConnFields) => void
  saved: { name: string }[]
  open: boolean
  onOpenChange: (v: boolean) => void
  onPickSaved: (i: number) => void
  onDeleteSaved: (i: number) => void
  onConnect: () => void
  onSave: () => void
  onDisconnect: () => void
  connected: boolean
  autoLogin: boolean
  onAutoLogin: (v: boolean) => void
  sessions: SessionInfo[]
  activeId: string
  onSwitch: (id: string) => void
  onDisconnectOne: (id: string) => void
  deadIds: Record<string, boolean>
  onReconnectOne: (id: string) => void
  onReconnectAll: () => void
}

export function CredentialManager(p: Props) {
  const { fields: f, setFields: set } = p
  const [savedKey, setSavedKey] = useState<string>('')
  const inp = (k: keyof ConnFields, placeholder?: string, type?: string) => (
    <Input
      placeholder={placeholder ?? k}
      type={type}
      value={f[k]}
      onChange={(e) => set({ ...f, [k]: e.target.value })}
      onKeyDown={(e) => {
        if (e.key === 'Enter') p.onConnect()
      }}
    />
  )

  const active = p.sessions.find((s) => s.id === p.activeId)

  return (
    <div className="border-b">
      <Popover open={p.open} onOpenChange={p.onOpenChange}>
        <PopoverTrigger asChild>
          <button className="-mb-px flex h-10 w-full items-center gap-1.5 px-2.5 text-left text-[12px] font-semibold hover:bg-accent">
            <span className={active ? 'text-emerald-500' : 'text-muted-foreground'} aria-hidden>
              ●
            </span>
            {active ? (
              <span className="min-w-0 flex-1 truncate font-semibold">
                {active.user}@{active.host}/{active.dbname}
                <span className="ml-1.5 font-normal text-muted-foreground">
                  {active.port !== '5432' ? `:${active.port}` : ''} · {p.sessions.length} session
                  {p.sessions.length === 1 ? '' : 's'}
                </span>
              </span>
            ) : (
              <>
                <KeyRound className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="flex-1">Connections</span>
              </>
            )}
            {!active && <ChevronsUpDown className="h-3.5 w-3.5 text-muted-foreground" />}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" side="bottom" sideOffset={4} className="max-h-[min(70vh,560px)] w-[min(320px,90vw)] overflow-y-auto">
          <div className="flex flex-col gap-1.5">
            {p.sessions.length > 0 && (
              <div className="flex flex-col gap-1">
                <div className="flex items-center gap-1.5 text-[11px] font-semibold text-muted-foreground">
                  <span className="flex-1">Active sessions ({p.sessions.length})</span>
                  {Object.keys(p.deadIds).length > 0 && (
                    <Tip content="Reconnect every lost session with its saved credentials">
                      <button className="flex items-center gap-1 hover:text-foreground" onClick={p.onReconnectAll}>
                        <RefreshCw className="h-3 w-3" /> Reconnect all
                      </button>
                    </Tip>
                  )}
                </div>
                {p.sessions.map((s) => {
                  const dead = !!p.deadIds[s.id]
                  return (
                    <div
                      key={s.id}
                      className={`flex items-center gap-1.5 rounded border px-1.5 py-1 text-[11px] ${s.id === p.activeId ? 'border-foreground/30 bg-accent' : 'border-border'}`}
                    >
                      <Tip content={`${s.user}@${s.host}:${s.port}/${s.dbname}${dead ? ' — pool lost (server restart?)' : ''}`}>
                        <button
                          className="flex min-w-0 flex-1 items-center gap-1.5 truncate text-left hover:underline"
                          onClick={() => p.onSwitch(s.id)}
                        >
                          <span className={dead ? 'text-red-400' : s.id === p.activeId ? '' : 'opacity-60'} aria-hidden>
                            {dead ? '○' : s.id === p.activeId ? '●' : '○'}
                          </span>
                          <span className="truncate">
                            {s.user}@{s.host}/{s.dbname}
                          </span>
                          {dead && <span className="shrink-0 rounded bg-red-500/15 px-1 text-[10px] text-red-400">dead</span>}
                        </button>
                      </Tip>
                      {dead && (
                        <Tip content="Reconnect this session with its saved credentials">
                          <Button size="sm" variant="ghost" aria-label="Reconnect this session" onClick={() => p.onReconnectOne(s.id)}>
                            <RefreshCw className="h-3 w-3" />
                          </Button>
                        </Tip>
                      )}
                      <Tip content="Disconnect this session">
                        <Button size="sm" variant="ghost" aria-label="Disconnect this session" onClick={() => p.onDisconnectOne(s.id)}>
                          <X className="h-3 w-3" />
                        </Button>
                      </Tip>
                    </div>
                  )
                })}
              </div>
            )}
            <div className="flex gap-1.5">
              <Select
                value={savedKey}
                onValueChange={(v) => {
                  setSavedKey(v)
                  p.onPickSaved(Number(v))
                }}
              >
                <SelectTrigger className="flex-1">
                  <SelectValue placeholder="— saved —" />
                </SelectTrigger>
                <SelectContent>
                  {p.saved.map((c, i) => (
                    <SelectItem key={i} value={String(i)}>
                      {c.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Tip content="Delete selected saved connection">
                <span className="inline-flex">
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label="Delete selected saved connection"
                    disabled={savedKey === '' || !p.saved[Number(savedKey)]}
                    onClick={() => {
                      p.onDeleteSaved(Number(savedKey))
                      setSavedKey('')
                    }}
                  >
                    <Trash2 />
                  </Button>
                </span>
              </Tip>
            </div>
            <div className="grid grid-cols-[1fr_64px] gap-1.5">
              {inp('host')}
              {inp('port')}
            </div>
            <div className="grid grid-cols-2 gap-1.5">
              {inp('user')}
              {inp('password', 'pass', 'password')}
            </div>
            <div className="grid grid-cols-[1fr_96px] gap-1.5">
              {inp('dbname', 'db')}
              <Select value={f.sslmode} onValueChange={(v) => set({ ...f, sslmode: v })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {['disable', 'prefer', 'require'].map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex gap-1.5">
              <Button size="sm" className="flex-1" onClick={p.onConnect}>
                {p.connected ? <PlugZap /> : <Plug />} Connect
              </Button>
              <Tip content="Save connection">
                <Button size="sm" variant="secondary" onClick={p.onSave} aria-label="Save connection">
                  <Save />
                </Button>
              </Tip>
              <Tip content="Disconnect">
                <Button size="sm" variant="ghost" onClick={p.onDisconnect} aria-label="Disconnect">
                  <X className="h-3 w-3" />
                </Button>
              </Tip>
            </div>
            <label className="flex cursor-pointer items-center justify-between gap-2 text-[12px] text-muted-foreground">
              <span>Auto-connect on startup</span>
              <Switch checked={p.autoLogin} onCheckedChange={p.onAutoLogin} />
            </label>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  )
}
