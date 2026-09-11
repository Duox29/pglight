import { useState } from 'react'
import { Plug, PlugZap, Save, Trash2, KeyRound, ChevronsUpDown } from 'lucide-react'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from './ui/collapsible'
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

  return (
    <Collapsible open={p.open} onOpenChange={p.onOpenChange} className="border-b">
      <CollapsibleTrigger asChild>
        <button className="-mb-px flex h-10 w-full items-center gap-1.5 px-2.5 text-left text-[12px] font-semibold hover:bg-accent">
          <KeyRound className="h-3.5 w-3.5 text-muted-foreground" />
          Connections
          <span className="flex-1" />
          <ChevronsUpDown className="h-3.5 w-3.5 text-muted-foreground" />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent className="data-[state=closed]:hidden">
        <div className="flex flex-col gap-1.5 px-2.5 pb-2.5">
          {p.sessions.length > 0 && (
            <div className="flex flex-col gap-1">
              <div className="text-[11px] font-semibold text-muted-foreground">Active sessions ({p.sessions.length})</div>
              {p.sessions.map((s) => (
                <div
                  key={s.id}
                  className={`flex items-center gap-1.5 rounded border px-1.5 py-1 text-[11px] ${s.id === p.activeId ? 'border-foreground/30 bg-accent' : 'border-border'}`}
                >
                  <Tip content={`${s.user}@${s.host}:${s.port}/${s.dbname}`}>
                    <button
                      className="min-w-0 flex-1 truncate text-left hover:underline"
                      onClick={() => p.onSwitch(s.id)}
                    >
                      {s.id === p.activeId ? '●' : '○'} {s.user}@{s.host}/{s.dbname}
                    </button>
                  </Tip>
                  <Tip content="Disconnect this session">
                    <Button size="sm" variant="ghost" aria-label="Disconnect this session" onClick={() => p.onDisconnectOne(s.id)}>
                      ✖
                    </Button>
                  </Tip>
                </div>
              ))}
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
                ✖
              </Button>
            </Tip>
          </div>
          <label className="flex cursor-pointer items-center justify-between gap-2 text-[12px] text-muted-foreground">
            <span>Auto-connect on startup</span>
            <Switch checked={p.autoLogin} onCheckedChange={p.onAutoLogin} />
          </label>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
