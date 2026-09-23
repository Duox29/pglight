import { useCallback, useEffect, useMemo, useState } from 'react'
import { LockKeyhole, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { SearchSelect } from './ui/search-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Switch } from './ui/switch'
import { Textarea } from './ui/textarea'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table'
import { ErrorText, EmptyNote } from './ui/feedback'
import type { DialogsApi } from './dialogs'
import { api, apiClient, q, type PrivilegeCatalog } from '@/lib/api'

const TABLE_PRIVILEGES = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']
type PendingChanges = { role: string; privilege: string; granted: boolean }[]
type PolicyPayload = { operation: string; schema: string; table: string; name?: string; command?: string; permissive?: string; roles?: string[]; using?: string; with_check?: string }
type PreviewPayload = { changes?: PendingChanges; policy?: PolicyPayload }

interface Props { session: string; dialogs: DialogsApi }

export function PrivilegeEditor({ session, dialogs }: Props) {
  const [schema, setSchema] = useState('')
  const [table, setTable] = useState('')
  const [schemas, setSchemas] = useState<string[]>([])
  const [relations, setRelations] = useState<{ schema: string; name: string; type: string }[]>([])
  const [catalog, setCatalog] = useState<PrivilegeCatalog | null>(null)
  const [changes, setChanges] = useState<{ role: string; privilege: string; granted: boolean }[]>([])
  const [error, setError] = useState('')
  const [preview, setPreview] = useState<string[]>([])
  const [previewAction, setPreviewAction] = useState('')
  const [previewPayload, setPreviewPayload] = useState<PreviewPayload>({})
  const [previewDescription, setPreviewDescription] = useState('')
  const [policyName, setPolicyName] = useState('')
  const [policyCommand, setPolicyCommand] = useState('SELECT')
  const [policyMode, setPolicyMode] = useState('PERMISSIVE')
  const [policyRoles, setPolicyRoles] = useState<string[]>([])
  const [usingExpression, setUsingExpression] = useState('')
  const [checkExpression, setCheckExpression] = useState('')
  const [policyOperation, setPolicyOperation] = useState('replace_policy')

  useEffect(() => {
    let cancelled = false
    if (!session) {
      setSchemas([])
      setRelations([])
      setSchema('')
      setTable('')
      setCatalog(null)
      return
    }
    void Promise.all([
      api<unknown>(q(session, '/api/schemas?')),
      api<unknown>(q(session, '/api/tables?')),
    ]).then(([schemaData, tableData]) => {
      if (cancelled) return
      const schemaNames = Array.isArray(schemaData) ? schemaData.filter((value): value is string => typeof value === 'string') : []
      const objects = Array.isArray(tableData)
        ? (tableData as { schema: string; name: string; type: string }[]).filter((item) => item.type === 'BASE TABLE' || item.type === 'VIEW' || item.type === 'FOREIGN')
        : []
      setSchemas(schemaNames)
      setRelations(objects)
      setSchema((current) => schemaNames.includes(current) ? current : schemaNames.includes('public') ? 'public' : schemaNames[0] ?? '')
      setTable('')
      setCatalog(null)
    }).catch((e: unknown) => {
      if (!cancelled) setError(e instanceof Error ? e.message : String(e))
    })
    return () => { cancelled = true }
  }, [session])

  const tableOptions = useMemo(() => relations
    .filter((item) => item.schema === schema)
    .map((item) => ({ value: item.name, label: `${item.name} · ${item.type === 'VIEW' ? 'view' : 'table'}` })), [relations, schema])

  const load = useCallback(async () => {
    if (!session || !table.trim() || !schema.trim()) return
    setError('')
    try { setCatalog(await apiClient.privileges(session, schema.trim(), table.trim())); setChanges([]); setPreview([]) }
    catch (e) { setError(e instanceof Error ? e.message : String(e)) }
  }, [schema, session, table])

  const granted = useMemo(() => new Set((catalog?.grants ?? []).map((x) => `${x.role}\0${x.privilege.toUpperCase()}`)), [catalog])
  const queueGrant = (role: string, privilege: string, next: boolean) => {
    const key = `${role}\0${privilege}`
    const baseline = granted.has(key)
    setChanges((items) => next === baseline ? items.filter((x) => `${x.role}\0${x.privilege}` !== key) : [...items.filter((x) => `${x.role}\0${x.privilege}` !== key), { role, privilege, granted: next }])
    setPreview([])
    setPreviewAction('')
    setPreviewPayload({})
  }

  const clearPreview = () => { setPreview([]); setPreviewAction(''); setPreviewPayload({}) }

  const requestPolicy = (operation: string): PolicyPayload => ({ operation, schema, table, name: policyName, command: policyCommand, permissive: policyMode, roles: policyRoles, using: usingExpression, with_check: checkExpression })
  const showPreview = async (action: string, payload: PreviewPayload, description: string) => {
    try {
      const result = await apiClient.privilegeAction({ session_id: session, action, schema, table, ...payload })
      if (result.error) throw new Error(result.error)
      setPreview(result.sql ?? [])
      setPreviewAction(action.replace('preview_', 'apply_'))
      setPreviewPayload(payload)
      setPreviewDescription(description)
    } catch (e) { toast.error(e instanceof Error ? e.message : String(e)) }
  }
  const applyPreview = async () => {
    if (!preview.length) { toast.error('Preview the SQL before applying'); return }
    const accepted = await dialogs.confirm({ title: 'Apply privilege change?', description: previewDescription, confirmText: 'Apply changes', danger: true })
    if (!accepted) return
    try {
      const result = await apiClient.privilegeAction({ session_id: session, action: previewAction, schema, table, ...previewPayload })
      if (result.error) throw new Error(result.error)
      toast.success('Privilege changes applied')
      setPreview([])
      setPreviewAction('')
      setPreviewPayload({})
      await load()
    } catch (e) { toast.error(e instanceof Error ? e.message : String(e)) }
  }

  const toggleRLS = async (operation: string, description: string) => {
    const policy = requestPolicy(operation)
    await showPreview('preview_policy', { policy }, description)
  }

  return <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
    <div className="text-sm font-semibold">Object privileges and row security</div>
    <Card className="flex flex-wrap items-end gap-2 p-3">
      <label className="grid gap-1 text-[11px] text-muted-foreground">Schema<SearchSelect value={schema} options={schemas.map((name) => ({ value: name, label: name }))} placeholder="Choose schema" emptyText="No schemas found" onChange={(value) => { setSchema(value); setTable(''); setCatalog(null); clearPreview() }} ariaLabel="Privilege schema" triggerClassName="w-[220px]" /></label>
      <label className="grid gap-1 text-[11px] text-muted-foreground">Table / view<SearchSelect value={table} options={tableOptions} placeholder="Choose table or view" emptyText="No tables or views in this schema" onChange={(value) => { setTable(value); setCatalog(null); clearPreview() }} ariaLabel="Privilege table" triggerClassName="w-[280px]" /></label>
      <Button size="sm" disabled={!session || !schema.trim() || !table.trim()} onClick={() => void load()}><RefreshCw /> Load object</Button>
    </Card>
    <ErrorText message={error} />
    {catalog && <>
      <Card className="p-3">
        <div className="mb-2 flex flex-wrap items-center gap-2"><LockKeyhole className="h-4 w-4" /><b className="flex-1 text-[12px]">Table privileges</b><Button size="sm" variant="secondary" disabled={!changes.length} onClick={() => void showPreview('preview_grants', { changes }, `Apply ${changes.length} GRANT or REVOKE changes to ${schema}.${table}?`)}>Preview SQL</Button><Button size="sm" variant="ghost" disabled={!changes.length} onClick={() => { setChanges([]); setPreview([]); setPreviewAction('') }}>Discard</Button></div>
        <Table containerClassName="max-h-72"><TableHeader className="bg-card"><TableRow><TableHead>Role</TableHead>{TABLE_PRIVILEGES.map((privilege) => <TableHead key={privilege} className="text-center">{privilege}</TableHead>)}</TableRow></TableHeader><TableBody>{[...(catalog.roles ?? []), { name: 'PUBLIC', superuser: false, create_role: false, create_db: false, login: false }].map((role) => <TableRow key={role.name}><TableCell className="font-medium">{role.name}</TableCell>{TABLE_PRIVILEGES.map((privilege) => { const pending = changes.find((x) => x.role === role.name && x.privilege === privilege); const checked = pending?.granted ?? granted.has(`${role.name}\0${privilege}`); return <TableCell key={privilege} className="text-center"><Switch checked={checked} onCheckedChange={(v) => queueGrant(role.name, privilege, v)} aria-label={`${privilege} grant for ${role.name}`} /></TableCell> })}</TableRow>)}</TableBody></Table>
        {!!changes.length && <p className="mt-2 text-[11px] text-muted-foreground">{changes.length} pending grant change(s)</p>}
      </Card>
      <Card className="p-3">
        <div className="mb-2 text-[12px] font-semibold">Row-level security</div>
        <div className="flex flex-wrap gap-2"><Button size="sm" variant={catalog.rls?.enabled ? 'secondary' : 'outline'} onClick={() => void toggleRLS(catalog.rls?.enabled ? 'disable_rls' : 'enable_rls', `${catalog.rls?.enabled ? 'Disable' : 'Enable'} row-level security on ${schema}.${table}?`)}>{catalog.rls?.enabled ? 'Disable RLS' : 'Enable RLS'}</Button><Button size="sm" variant="outline" onClick={() => void toggleRLS(catalog.rls?.forced ? 'no_force_rls' : 'force_rls', `${catalog.rls?.forced ? 'Stop forcing' : 'Force'} row-level security for table owners?`)}>{catalog.rls?.forced ? 'Unforce RLS' : 'Force RLS'}</Button><span className="self-center text-[11px] text-muted-foreground">Enabled: {String(!!catalog.rls?.enabled)} · Forced: {String(!!catalog.rls?.forced)}</span></div>
        <div className="mt-3 grid gap-2 sm:grid-cols-2"><Input value={policyName} onChange={(e) => { setPolicyName(e.target.value); setPolicyOperation('replace_policy'); clearPreview() }} placeholder="Policy name" aria-label="Policy name" /><Select value={policyCommand} onValueChange={(v) => { setPolicyCommand(v); clearPreview() }}><SelectTrigger aria-label="Policy command"><SelectValue /></SelectTrigger><SelectContent>{['ALL', 'SELECT', 'INSERT', 'UPDATE', 'DELETE'].map((x) => <SelectItem key={x} value={x}>{x}</SelectItem>)}</SelectContent></Select><Select value={policyMode} onValueChange={(v) => { setPolicyMode(v); clearPreview() }}><SelectTrigger aria-label="Policy mode"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="PERMISSIVE">PERMISSIVE</SelectItem><SelectItem value="RESTRICTIVE">RESTRICTIVE</SelectItem></SelectContent></Select><Select value={policyRoles[0] ?? ''} onValueChange={(v) => { setPolicyRoles([v]); clearPreview() }}><SelectTrigger aria-label="Policy role"><SelectValue placeholder="Choose role" /></SelectTrigger><SelectContent>{catalog.roles.map((role) => <SelectItem key={role.name} value={role.name}>{role.name}</SelectItem>)}<SelectItem value="PUBLIC">PUBLIC</SelectItem></SelectContent></Select><Textarea value={usingExpression} onChange={(e) => { setUsingExpression(e.target.value); clearPreview() }} placeholder="USING expression" aria-label="Policy USING expression" /><Textarea value={checkExpression} onChange={(e) => { setCheckExpression(e.target.value); clearPreview() }} placeholder="WITH CHECK expression" aria-label="Policy WITH CHECK expression" /></div>
        <div className="mt-2 flex gap-2"><Button size="sm" variant="secondary" disabled={!policyName || (policyOperation === 'replace_policy' && !policyRoles.length)} onClick={() => void showPreview('preview_policy', { policy: requestPolicy(policyOperation) }, `${policyOperation === 'drop_policy' ? 'Drop' : 'Create or replace'} policy ${policyName} on ${schema}.${table}?`)}>Preview policy SQL</Button></div>
        <div className="mt-3 flex flex-col gap-1">{catalog.rls?.policies?.map((policy) => <div key={policy.name} className="rounded border p-2 text-[11px]"><b>{policy.name}</b> · {policy.command} · {policy.roles.join(', ')}<pre className="mt-1 whitespace-pre-wrap text-muted-foreground">USING {policy.using || '—'} · WITH CHECK {policy.with_check || '—'}</pre><Button size="sm" variant="ghost" onClick={() => { setPolicyName(policy.name); setPolicyOperation('drop_policy'); clearPreview() }}>Prepare drop</Button></div>)}{!catalog.rls?.policies?.length && <EmptyNote text="No row security policies" />}</div>
      </Card>
    </>}
    <Dialog open={preview.length > 0} onOpenChange={(open) => { if (!open) clearPreview() }}>
      <DialogContent className="max-h-[80vh] overflow-auto">
        <DialogHeader><DialogTitle>SQL preview</DialogTitle></DialogHeader>
        <pre className="max-h-[52vh] overflow-auto whitespace-pre-wrap rounded border bg-muted/30 p-3 text-[11px]">{preview.join(';\n')};</pre>
        <div className="flex justify-end gap-2"><Button size="sm" variant="outline" onClick={clearPreview}>Close</Button><Button size="sm" onClick={() => void applyPreview()}>Apply previewed SQL</Button></div>
      </DialogContent>
    </Dialog>
  </div>
}
