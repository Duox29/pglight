import { useMemo, useState, type ReactElement } from 'react'
import { Copy, KeyRound, Lock, LockOpen, Plus, RefreshCw, Save, ShieldCheck, Trash2, Unlock, X } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { Input } from './ui/input'
import { PasswordTextarea } from './ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Switch } from './ui/switch'
import { Tip } from './ui/tooltip'
import { EmptyNote, ErrorText } from './ui/feedback'
import type { ConnFields } from './ConnectionBar'
import type { DialogsApi } from './dialogs'
import type { SavedConnection, SessionInfo } from '@/types'
import { apiClient } from '@/lib/api'

export interface ProfileMetadata {
  folder_id: string
  environment: string
  color: string
  description: string
  favorite: boolean
  default: boolean
  tags: string[]
  connect_timeout: number
  keepalive: number
  application_name: string
  search_path: string
  sslrootcert: string
  sslcert: string
  sslkey: string
  unix_socket: string
  ssh_enabled: boolean
  ssh_host: string
  ssh_port: number
  ssh_user: string
  ssh_auth_method: string
  ssh_host_key: string
  ssh_password: string
  ssh_private_key: string
  ssh_passphrase: string
}

export function isInsecureTls(host: string, sslmode: string): boolean {
  const h = host.trim().toLowerCase()
  if (!h || h === 'localhost' || h === '127.0.0.1' || h === '::1') return false
  if (/^127\./.test(h)) return false
  const m = sslmode.trim().toLowerCase()
  return m !== 'verify-ca' && m !== 'verify-full'
}

function FieldTip({ content, children }: { content: string; children: ReactElement }) {
  return <Tip content={content} side="top" align="start">{children}</Tip>
}

function SwitchTip({ content, children }: { content: string; children: ReactElement }) {
  return <Tip content={content} side="top" align="start"><span className="inline-flex">{children}</span></Tip>
}

interface Props {
  fields: ConnFields
  setFields: (f: ConnFields) => void
  saved: SavedConnection[]
  onConnect: (profileId?: string, sshSecrets?: { password?: string; private_key?: string; passphrase?: string }) => Promise<string>
  onTest: (profileId?: string, password?: string) => Promise<{ database?: string; version?: string; latency_ms?: number }>
  onSave: (name: string, savePassword: boolean, clearPassword: boolean, metadata: ProfileMetadata) => Promise<string | void>
  onDuplicate: (name: string) => Promise<string>
  onDelete: (id: string) => Promise<void>
  vault: { exists: boolean; unlocked: boolean }
  onVaultAction: (action: 'setup' | 'unlock' | 'lock' | 'change_password', masterPassword?: string, newPassword?: string) => Promise<void>
  dialogs: DialogsApi
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
  const [selectedId, setSelectedId] = useState('')
  const [name, setName] = useState('')
  const [filter, setFilter] = useState('')
  const [advanced, setAdvanced] = useState(false)
  const [connectionError, setConnectionError] = useState('')
  const emptyMetadata = (): ProfileMetadata => ({ folder_id: '', environment: '', color: '', description: '', favorite: false, default: false, tags: [], connect_timeout: 5, keepalive: 30, application_name: '', search_path: '', sslrootcert: '', sslcert: '', sslkey: '', unix_socket: '', ssh_enabled: false, ssh_host: '', ssh_port: 22, ssh_user: '', ssh_auth_method: 'password', ssh_host_key: '', ssh_password: '', ssh_private_key: '', ssh_passphrase: '' })
  const [metadata, setMetadata] = useState<ProfileMetadata>(emptyMetadata())
  const selected = useMemo(() => p.saved.find((x) => x.id === selectedId), [p.saved, selectedId])

  const choose = (c: SavedConnection) => {
    setConnectionError('')
    setSelectedId(c.id ?? '')
    setName(c.name)
    setMetadata({ ...emptyMetadata(), folder_id: c.folder_id ?? '', environment: c.environment ?? '', color: c.color ?? '', description: c.description ?? '', favorite: !!c.favorite, default: !!c.default, tags: c.tags ?? [], connect_timeout: c.options?.connect_timeout ?? 5, keepalive: c.options?.keepalive ?? 30, application_name: c.options?.application_name ?? '', search_path: c.options?.search_path ?? '', sslrootcert: c.options?.sslrootcert ?? '', sslcert: c.options?.sslcert ?? '', sslkey: c.options?.sslkey ?? '', unix_socket: c.options?.unix_socket ?? '', ssh_enabled: c.options?.ssh_enabled ?? false, ssh_host: c.options?.ssh_host ?? '', ssh_port: c.options?.ssh_port ?? 22, ssh_user: c.options?.ssh_user ?? '', ssh_auth_method: c.options?.ssh_auth_method ?? 'password', ssh_host_key: c.options?.ssh_host_key ?? '' })
    p.setFields({ host: c.host, port: c.port, user: c.user, password: '', dbname: c.dbname, sslmode: c.sslmode, profileId: c.id })
  }

  const fresh = () => {
    setConnectionError('')
    setSelectedId('')
    setName('')
    setMetadata(emptyMetadata())
    p.setFields({ ...p.fields, password: '', profileId: undefined })
  }

  const pinUntrustedHostKey = async (message: string): Promise<boolean> => {
    const fingerprint = message.match(/SSH host key is not trusted; fingerprint: (SHA256:[^\s]+)/)?.[1]
    if (!fingerprint || !selectedId || !selected) return false
    const trust = await p.dialogs.confirm({ title: 'Verify SSH server identity', description: `The SSH server presented ${fingerprint}. Verify this fingerprint with your server administrator through a trusted channel before accepting it. Pin this key to the selected profile?`, confirmText: 'Pin verified key' })
    if (!trust) return false
    const next = { ...metadata, ssh_host_key: fingerprint }
    setMetadata(next)
    await p.onSave(name.trim(), false, false, next)
    setMetadata((current) => ({ ...current, ssh_password: '', ssh_private_key: '', ssh_passphrase: '' }))
    return true
  }

  const vaultDialog = async (action: 'setup' | 'unlock' | 'change_password') => {
    try {
      if (action === 'setup') {
        const values = await p.dialogs.form({
          title: 'Create credential vault',
          description: 'The master password unlocks saved PostgreSQL and SSH credentials. It cannot be recovered.',
          fields: [
            { key: 'master', label: 'Master password', placeholder: 'At least 8 characters', type: 'password' },
            { key: 'confirm', label: 'Confirm master password', placeholder: 'Repeat master password', type: 'password' },
          ],
          submitText: 'Create vault',
        })
        if (!values || values.master !== values.confirm) {
          if (values) toast.error('Master passwords do not match')
          return
        }
        await p.onVaultAction('setup', values.master ?? undefined)
        toast.success('Vault created and unlocked')
      } else if (action === 'change_password') {
        const values = await p.dialogs.form({
          title: 'Change vault master password',
          description: 'Saved database passwords are re-encrypted atomically with the new master password.',
          fields: [
            { key: 'current', label: 'Current master password', placeholder: 'Current password', type: 'password' },
            { key: 'next', label: 'New master password', placeholder: 'At least 8 characters', type: 'password' },
            { key: 'confirm', label: 'Confirm new password', placeholder: 'Repeat new password', type: 'password' },
          ],
          submitText: 'Change password',
        })
        if (!values || values.current == null || values.next == null) return
        if (values.next !== values.confirm) { toast.error('New master passwords do not match'); return }
        await p.onVaultAction('change_password', values.current, values.next)
        toast.success('Vault master password changed')
      } else {
        const values = await p.dialogs.form({
          title: 'Unlock credential vault',
          description: 'Enter the vault master password.',
          fields: [{ key: 'master', label: 'Master password', placeholder: 'Master password', type: 'password' }],
          submitText: 'Unlock',
        })
        if (!values || values.master == null) return
        await p.onVaultAction(action, values.master)
        toast.success('Vault unlocked')
      }
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      toast.error(message)
    }
  }

  const save = async () => {
    if (!name.trim()) {
      toast.error('Enter a profile name')
      return
    }
    try {
      const id = await p.onSave(name.trim(), p.fields.password.length > 0, false, metadata)
      if (id) setSelectedId(id)
      setMetadata((current) => ({ ...current, ssh_password: '', ssh_private_key: '', ssh_passphrase: '' }))
      toast.success('Connection profile saved')
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      toast.error(message)
    }
  }

  const duplicate = async () => {
    if (!selected) return
    const copyName = `${selected.name} copy`
    try {
      const id = await p.onDuplicate(copyName)
      setSelectedId(id)
      setName(copyName)
      p.setFields({ ...p.fields, password: '', profileId: id })
      toast.success('Connection profile duplicated')
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      toast.error(message)
    }
  }

  const connect = async () => {
    try {
      setConnectionError('')
      await p.onConnect(selectedId || undefined)
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      if (selected?.options?.ssh_enabled && /vault is locked|no SSH credentials saved/i.test(message)) {
        const privateKey = selected.options.ssh_auth_method === 'private_key'
        const fields = privateKey
          ? [
              { key: 'private_key', label: 'SSH private key', placeholder: 'Paste private key', type: 'password', multiline: true },
              { key: 'passphrase', label: 'Key passphrase', placeholder: 'Optional passphrase', type: 'password' },
            ]
          : [{ key: 'password', label: 'SSH password', placeholder: 'SSH password', type: 'password' }]
        const values = await p.dialogs.form({
          title: 'Enter SSH credentials',
          description: 'These credentials are used for this connection only. Unlock the vault to use saved SSH credentials or save them in Advanced settings.',
          fields,
          submitText: 'Connect',
        })
        if (!values) return
        try {
          setConnectionError('')
          await p.onConnect(selectedId, { password: values.password ?? undefined, private_key: values.private_key ?? undefined, passphrase: values.passphrase ?? undefined })
          return
        } catch (retryError) {
          const retryMessage = retryError instanceof Error ? retryError.message : String(retryError)
          if (await pinUntrustedHostKey(retryMessage)) {
            try {
              const sshSecrets = { password: values.password ?? undefined, private_key: values.private_key ?? undefined, passphrase: values.passphrase ?? undefined }
              await p.onConnect(selectedId, sshSecrets)
              return
            } catch (pinRetryError) {
              const pinRetryMessage = pinRetryError instanceof Error ? pinRetryError.message : String(pinRetryError)
              setConnectionError(pinRetryMessage)
              toast.error(pinRetryMessage)
              return
            }
          }
          setConnectionError(retryMessage)
          toast.error(retryMessage)
          return
        }
      }
      if (await pinUntrustedHostKey(message)) {
        try {
          await p.onConnect(selectedId)
          return
        } catch (pinRetryError) {
          const pinRetryMessage = pinRetryError instanceof Error ? pinRetryError.message : String(pinRetryError)
          setConnectionError(pinRetryMessage)
          toast.error(pinRetryMessage)
          return
        }
      }
      setConnectionError(message)
      toast.error(message)
    }
  }

  const test = async () => {
    try {
      setConnectionError('')
      const result = await p.onTest(selectedId || undefined, p.fields.password)
      toast.success(`Connection test succeeded · ${result.database ?? 'database'} · PostgreSQL ${result.version ?? 'unknown'} · ${result.latency_ms ?? 0} ms`)
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      setConnectionError(message)
      if (await pinUntrustedHostKey(message)) {
        try {
          const result = await p.onTest(selectedId, p.fields.password)
          setConnectionError('')
          toast.success(`Connection test succeeded · ${result.database ?? 'database'} · PostgreSQL ${result.version ?? 'unknown'} · ${result.latency_ms ?? 0} ms`)
          return
        } catch (retryError) {
          const retryMessage = retryError instanceof Error ? retryError.message : String(retryError)
          setConnectionError(retryMessage)
          toast.error(retryMessage)
          return
        }
      }
      toast.error(message)
    }
  }

  const remove = async () => {
    if (!selected) return
    const ok = await p.dialogs.confirm({
      title: `Delete ${selected.name}?`,
      description: 'This removes the connection profile and its encrypted PostgreSQL and SSH credentials.',
      confirmText: 'Delete',
      danger: true,
    })
    if (!ok) return
    try {
      await p.onDelete(selected.id ?? '')
      fresh()
      toast.success('Connection profile deleted')
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      toast.error(message)
    }
  }

  const visible = p.saved.filter((c) => !filter || `${c.name} ${c.host} ${c.dbname} ${c.user} ${c.environment ?? ''} ${c.options?.ssh_host ?? ''} ${c.options?.ssh_user ?? ''} ${(c.tags ?? []).join(' ')}`.toLowerCase().includes(filter.toLowerCase()))
  const deadCount = Object.keys(p.deadIds).length
  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-auto p-3">
      <div>
        <div className="text-sm font-semibold">Connections</div>
      </div>

      <Card className="shrink-0 p-3">
        <div className="flex items-center gap-2">
          {p.vault.unlocked ? <ShieldCheck className="h-4 w-4 text-emerald-500" /> : <Lock className="h-4 w-4 text-amber-500" />}
          <div className="min-w-0 flex-1">
            <div className="text-[12px] font-semibold">Credential vault</div>
            <div className="text-[11px] text-muted-foreground">
              {!p.vault.exists ? 'Vault not configured' : p.vault.unlocked ? 'Vaut unlocked' : 'Vault locked'}
            </div>
          </div>
          {!p.vault.exists && <Button size="sm" onClick={() => void vaultDialog('setup')}><KeyRound /> Create</Button>}
          {p.vault.exists && !p.vault.unlocked && <Button size="sm" onClick={() => void vaultDialog('unlock')}><Unlock /> Unlock</Button>}
          {p.vault.unlocked && <><Button size="sm" variant="ghost" onClick={() => void p.onVaultAction('lock').then(() => p.setFields({ ...p.fields, password: '' })).catch((e) => toast.error(e instanceof Error ? e.message : String(e)))}><Lock /> Lock</Button><Button size="sm" variant="ghost" onClick={() => void vaultDialog('change_password')}><KeyRound /> Change password</Button></>}
        </div>
        {p.vault.exists && <div className="mt-2 border-t pt-2 text-[11px] text-muted-foreground">Forgot the master password? Close pglight, then run <code>pglight.exe vault reset</code> to remove saved database and SSH credentials while keeping profiles.</div>}
      </Card>
      <div className="grid shrink-0 min-h-0 gap-3 lg:grid-cols-[minmax(220px,0.8fr)_minmax(320px,1.2fr)]">
        <Card className="min-h-[240px] p-2">
          <div className="mb-2 flex items-center gap-1.5">
            <FieldTip content="Filter saved profiles by name, host, database, user, environment, or tags."><Input placeholder="Filter profiles…" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter connection profiles" /></FieldTip>
            <Tip content="New connection profile"><Button size="icon" variant="ghost" aria-label="New connection profile" onClick={fresh}><Plus /></Button></Tip>
          </div>
          <div className="flex flex-col gap-1">
            {visible.map((c) => (
              <Button key={c.id} variant="ghost" className={`h-auto w-full justify-start rounded border px-2 py-1.5 text-left text-[12px] ${c.id === selectedId ? 'border-primary bg-accent' : 'border-border hover:bg-accent'}`} onClick={() => choose(c)}>
                <span className="flex items-center gap-1.5"><KeyRound className="h-3 w-3 shrink-0 text-muted-foreground" /><span className="min-w-0 flex-1 truncate font-medium">{c.name}</span>{c.options?.ssh_enabled && <Badge variant="secondary" className="px-1 py-0 text-[9px]">SSH</Badge>}{c.has_password && <LockOpen className="h-3 w-3 shrink-0 text-emerald-500" />}</span>
                <span className="mt-0.5 block truncate text-[10px] text-muted-foreground">{c.user}@{c.host}:{c.port}/{c.dbname}</span>
              </Button>
            ))}
            {!visible.length && <EmptyNote text={filter ? 'No matching profiles' : 'No saved profiles'} />}
          </div>
        </Card>

        <Card className="p-3">
          <div className="mb-2 flex min-h-7 min-w-0 flex-nowrap items-center gap-2"><div className="min-w-0 flex-1 truncate text-[12px] font-semibold">{selected ? `Edit ${selected.name}` : 'New connection'}</div>{selected && <div className="flex shrink-0 items-center gap-1.5"><Button size="sm" variant="ghost" onClick={() => void duplicate()}><Copy /> Duplicate</Button><Button size="sm" variant="ghost" onClick={fresh}><Plus /> New</Button></div>}<div className="shrink-0"><FieldTip content="Simple shows the connection essentials. Advanced reveals optional profile metadata and PostgreSQL connection options."><label className="flex items-center gap-1.5 text-[11px] text-muted-foreground"><span className={!advanced ? 'font-medium text-foreground' : undefined}>Simple</span><Switch checked={advanced} onCheckedChange={setAdvanced} aria-label="Advanced connection form" /><span className={advanced ? 'font-medium text-foreground' : undefined}>Advanced</span></label></FieldTip></div></div>
          <div className="grid gap-1.5 sm:grid-cols-[1fr_90px]"><FieldTip content="Friendly name used for this saved connection profile."><Input placeholder="Profile name" value={name} onChange={(e) => setName(e.target.value)} aria-label="Profile name" /></FieldTip><FieldTip content="PostgreSQL port. The default is 5432."><Input placeholder="Port" value={p.fields.port} onChange={(e) => p.setFields({ ...p.fields, port: e.target.value })} aria-label="Port" /></FieldTip></div>
          <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><FieldTip content="Database server hostname, IP address, or Unix socket host."><Input placeholder="Host" value={p.fields.host} onChange={(e) => p.setFields({ ...p.fields, host: e.target.value })} aria-label="Host" /></FieldTip><FieldTip content="Name of the PostgreSQL database to open."><Input placeholder="Database" value={p.fields.dbname} onChange={(e) => p.setFields({ ...p.fields, dbname: e.target.value })} aria-label="Database" /></FieldTip></div>
          <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><FieldTip content="PostgreSQL role used to authenticate."><Input placeholder="User" value={p.fields.user} onChange={(e) => p.setFields({ ...p.fields, user: e.target.value })} aria-label="User" /></FieldTip><FieldTip content="Password for this connection. It is saved only when you explicitly save it to the credential vault."><Input placeholder={selected?.has_password ? 'Password (vault)' : 'Password'} type="password" value={p.fields.password} onChange={(e) => p.setFields({ ...p.fields, password: e.target.value })} aria-label="Password" /></FieldTip></div>
          <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><Select value={p.fields.sslmode} onValueChange={(v) => p.setFields({ ...p.fields, sslmode: v })}><FieldTip content="TLS behavior: prefer negotiates TLS when available; verify-ca and verify-full require certificate verification."><SelectTrigger aria-label="SSL mode"><SelectValue /></SelectTrigger></FieldTip><SelectContent>{['disable', 'prefer', 'require', 'verify-ca', 'verify-full'].map((m) => <SelectItem key={m} value={m}>{m}</SelectItem>)}</SelectContent></Select><FieldTip content={selected?.has_password ? 'This profile has a password stored in the credential vault.' : 'No password is currently stored in the credential vault.'}><div className="flex items-center text-[11px] text-muted-foreground">{selected?.has_password ? <><LockOpen className="mr-1 h-3 w-3 text-emerald-500" /> Saved in vault</> : 'No saved password'}</div></FieldTip></div>
          {advanced && <>
            <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><FieldTip content="Optional environment label such as dev, staging, or prod."><Input placeholder="Environment (dev/staging/prod)" value={metadata.environment} onChange={(e) => setMetadata({ ...metadata, environment: e.target.value })} aria-label="Environment" /></FieldTip><FieldTip content="Optional folder identifier used to organize saved profiles."><Input placeholder="Folder id (optional)" value={metadata.folder_id} onChange={(e) => setMetadata({ ...metadata, folder_id: e.target.value })} aria-label="Folder id" /></FieldTip></div>
            <FieldTip content="Optional comma-separated tags for filtering saved profiles."><Input className="mt-1.5" placeholder="Tags, comma separated" value={metadata.tags.join(', ')} onChange={(e) => setMetadata({ ...metadata, tags: e.target.value.split(',').map((x) => x.trim()).filter(Boolean) })} aria-label="Tags" /></FieldTip>
            <FieldTip content="Optional notes shown only in pglight. This is not sent to PostgreSQL."><Input className="mt-1.5" placeholder="Description" value={metadata.description} onChange={(e) => setMetadata({ ...metadata, description: e.target.value })} aria-label="Description" /></FieldTip>
            <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><FieldTip content="Maximum time, in seconds, to establish a new connection."><Input type="number" min={0} max={300} placeholder="Connection timeout (s)" value={metadata.connect_timeout} onChange={(e) => setMetadata({ ...metadata, connect_timeout: Number(e.target.value) || 0 })} aria-label="Connection timeout" /></FieldTip><FieldTip content="TCP keepalive interval, in seconds. Use 0 to leave the driver default."><Input type="number" min={0} max={86400} placeholder="Keepalive (s)" value={metadata.keepalive} onChange={(e) => setMetadata({ ...metadata, keepalive: Number(e.target.value) || 0 })} aria-label="Keepalive" /></FieldTip></div>
            <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><FieldTip content="Application name reported to PostgreSQL activity and logs."><Input placeholder="Application name" value={metadata.application_name} onChange={(e) => setMetadata({ ...metadata, application_name: e.target.value })} aria-label="Application name" /></FieldTip><FieldTip content="PostgreSQL startup search_path, for example public, extensions."><Input placeholder="Startup search_path" value={metadata.search_path} onChange={(e) => setMetadata({ ...metadata, search_path: e.target.value })} aria-label="Startup search_path" /></FieldTip></div>
            <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><FieldTip content="Optional Unix socket directory used instead of TCP host lookup."><Input placeholder="Unix socket path (optional)" value={metadata.unix_socket} onChange={(e) => setMetadata({ ...metadata, unix_socket: e.target.value })} aria-label="Unix socket path" /></FieldTip><FieldTip content="Path to the CA certificate used for PostgreSQL TLS verification."><Input placeholder="SSL CA/client cert paths" value={metadata.sslrootcert} onChange={(e) => setMetadata({ ...metadata, sslrootcert: e.target.value })} aria-label="SSL CA/client cert paths" /></FieldTip></div>
            <div className="mt-3 rounded-md border p-2.5">
              <label className="flex items-center justify-between gap-2 text-xs font-medium"><span>SSH tunnel</span><Switch checked={metadata.ssh_enabled} onCheckedChange={(v) => setMetadata({ ...metadata, ssh_enabled: v })} aria-label="Use SSH tunnel" /></label>
              {metadata.ssh_enabled && <>
                <p className="mb-2 mt-1 text-[11px] text-muted-foreground">PostgreSQL host below is resolved from the SSH server. SSH protects the route; PostgreSQL TLS settings above remain independent.</p>
                <div className="grid gap-1.5 sm:grid-cols-[1fr_84px]"><Input placeholder="SSH host" value={metadata.ssh_host} onChange={(e) => setMetadata({ ...metadata, ssh_host: e.target.value })} aria-label="SSH host" /><Input type="number" min={1} max={65535} value={metadata.ssh_port} onChange={(e) => setMetadata({ ...metadata, ssh_port: Number(e.target.value) || 22 })} aria-label="SSH port" /></div>
                <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2"><Input placeholder="SSH username" value={metadata.ssh_user} onChange={(e) => setMetadata({ ...metadata, ssh_user: e.target.value })} aria-label="SSH username" /><Select value={metadata.ssh_auth_method} onValueChange={(v) => setMetadata({ ...metadata, ssh_auth_method: v })}><SelectTrigger aria-label="SSH authentication"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="password">Password</SelectItem><SelectItem value="private_key">Private key</SelectItem></SelectContent></Select></div>
                {metadata.ssh_auth_method === 'password' ? <Input className="mt-1.5" type="password" placeholder="SSH password (saved to vault)" value={metadata.ssh_password} onChange={(e) => setMetadata({ ...metadata, ssh_password: e.target.value })} aria-label="SSH password" /> : <><PasswordTextarea className="mt-1.5 min-h-24 font-mono text-xs" placeholder="Paste SSH private key (saved to vault)" value={metadata.ssh_private_key} onChange={(e) => setMetadata({ ...metadata, ssh_private_key: e.target.value })} aria-label="SSH private key" /><Input className="mt-1.5" type="password" placeholder="Key passphrase (optional)" value={metadata.ssh_passphrase} onChange={(e) => setMetadata({ ...metadata, ssh_passphrase: e.target.value })} aria-label="SSH key passphrase" /></>}
                <FieldTip content="SHA256 fingerprint from a trusted SSH host key. First connection reports the fingerprint; verify it out of band, then paste it here before connecting again."><Input className="mt-1.5" placeholder="Trusted host key SHA256 fingerprint" value={metadata.ssh_host_key} onChange={(e) => setMetadata({ ...metadata, ssh_host_key: e.target.value })} aria-label="Trusted SSH host key fingerprint" /></FieldTip>
              </>}
            </div>
            <div className="mt-1.5 flex gap-4 text-[11px] text-muted-foreground"><label className="flex items-center gap-1"><SwitchTip content="Mark this profile as a favorite for easier discovery."><Switch checked={metadata.favorite} onCheckedChange={(v) => setMetadata({ ...metadata, favorite: v })} aria-label="Favorite" /></SwitchTip> Favorite</label><label className="flex items-center gap-1"><SwitchTip content="Use this profile as the default when pglight starts or opens a connection."><Switch checked={metadata.default} onCheckedChange={(v) => setMetadata({ ...metadata, default: v })} aria-label="Default" /></SwitchTip> Default</label></div>
          </>}
          {isInsecureTls(p.fields.host, p.fields.sslmode) && <p className="mt-2 rounded border border-amber-500/40 bg-amber-500/10 px-1.5 py-1 text-[11px] text-amber-600 dark:text-amber-400">Non-local host without certificate verification.</p>}
          <div className="mt-2 flex flex-wrap gap-1.5"><Button size="sm" onClick={() => void connect()}><Unlock /> Connect</Button><Button size="sm" variant="secondary" onClick={() => void test()}><RefreshCw /> Test</Button><Button size="sm" variant="secondary" onClick={() => void save()}><Save /> Save</Button>{selected && <Button size="sm" variant="ghost" className="text-destructive" onClick={() => void remove()}><Trash2 /> Delete</Button>}</div>
          {connectionError && <div className="mt-2 rounded border border-destructive/40 bg-destructive/5 px-2 py-1.5 text-xs" role="alert"><ErrorText message={connectionError} /></div>}
          <label className="mt-2 flex items-center justify-between gap-2 text-[11px] text-muted-foreground"><span>Auto-connect on startup (requires unlocked vault for saved passwords)</span><SwitchTip content="Automatically connect to the default profile when pglight starts, if the vault is unlocked."><Switch checked={p.autoLogin} onCheckedChange={p.onAutoLogin} aria-label="Auto-connect on startup" /></SwitchTip></label>
        </Card>
      </div>

      <Card className="shrink-0 p-3">
        <div className="mb-2 flex items-center gap-2 text-[12px] font-semibold"><span className="flex-1">Active sessions ({p.sessions.length})</span>{deadCount > 0 && <Button size="sm" variant="ghost" onClick={p.onReconnectAll}><RefreshCw /> Reconnect all</Button>}</div>
        <div className="flex flex-col gap-1.5">{p.sessions.map((s) => { const dead = !!p.deadIds[s.id]; return <div key={s.id} className="flex items-center gap-1.5 rounded border px-2 py-1.5 text-[11px]"><Button size="sm" variant="ghost" className="min-w-0 flex-1 justify-start truncate" onClick={() => p.onSwitch(s.id)}><span className={dead ? 'text-red-400' : 'text-emerald-500'}>●</span><span className="truncate">{s.profile_name ? `${s.profile_name} · ` : ''}{s.user}@{s.host}:{s.port}/{s.dbname}</span>{s.ssh_tunnel && <Badge variant="secondary" className="shrink-0 px-1 py-0 text-[9px]">SSH</Badge>}</Button>{dead && <Button size="sm" variant="ghost" aria-label="Reconnect session" onClick={() => p.onReconnectOne(s.id)}><RefreshCw /></Button>}<Button size="sm" variant="ghost" aria-label="Disconnect session" onClick={() => p.onDisconnectOne(s.id)}><X /></Button></div> })}</div>
        {!p.sessions.length && <EmptyNote text="No active sessions" />}
      </Card>
    </div>
  )
}
