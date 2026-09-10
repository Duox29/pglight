import { useState } from 'react'
import { Plug, PlugZap, Save, Trash2, KeyRound, ChevronsUpDown } from 'lucide-react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Badge } from './ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from './ui/collapsible'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
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
        <button className="flex w-full items-center gap-1.5 px-2.5 py-2 text-left text-[12px] font-semibold hover:bg-accent">
          <KeyRound className="h-3.5 w-3.5 text-muted-foreground" />
          Connections
          <Badge variant={p.connected ? 'success' : 'secondary'} className="ml-1">
            {p.connected ? 'connected' : 'disconnected'}
          </Badge>
          <span className="flex-1" />
          <ChevronsUpDown className="h-3.5 w-3.5 text-muted-foreground" />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent className="data-[state=closed]:hidden">
        <div className="flex flex-col gap-1.5 px-2.5 pb-2.5">
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
            <Button
              size="icon"
              variant="ghost"
              title="Delete selected saved connection"
              disabled={savedKey === '' || !p.saved[Number(savedKey)]}
              onClick={() => {
                p.onDeleteSaved(Number(savedKey))
                setSavedKey('')
              }}
            >
              <Trash2 />
            </Button>
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
            <Button size="sm" variant="secondary" onClick={p.onSave} title="Save connection">
              <Save />
            </Button>
            <Button size="sm" variant="ghost" onClick={p.onDisconnect} title="Disconnect">
              ✖
            </Button>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
