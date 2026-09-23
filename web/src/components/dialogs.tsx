import { useState } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from './ui/alert-dialog'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from './ui/dialog'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { PasswordTextarea, Textarea } from './ui/textarea'
import { Tip } from './ui/tooltip'

/** Promise-based popups (shadcn AlertDialog / Dialog) replacing native confirm()/prompt(). */

export interface FormField {
  key: string
  label: string
  placeholder?: string
  type?: string
  multiline?: boolean
  /** When true the field renders a NULL toggle; a nulled field resolves to null. */
  allowNull?: boolean
}

export interface PendingDialog {
  id: number
  kind: 'confirm' | 'prompt' | 'form'
  title: string
  description?: string
  confirmText?: string
  submitText?: string
  danger?: boolean
  placeholder?: string
  defaultValue?: string
  /** Prompt shows an extra "Set NULL" action resolving { isNull: true }. */
  nullable?: boolean
  fields?: FormField[]
  resolve: (v: boolean | string | Record<string, string | null> | { isNull: boolean } | null) => void
}

export interface DialogsApi {
  confirm: (o: { title: string; description?: string; confirmText?: string; danger?: boolean }) => Promise<boolean>
  prompt: (o: { title: string; description?: string; placeholder?: string; defaultValue?: string; submitText?: string }) => Promise<string | null>
  /** Like prompt, but the user can explicitly pick NULL (returned as { isNull: true }); null still means cancelled. */
  promptNullable: (o: { title: string; description?: string; placeholder?: string; defaultValue?: string; submitText?: string }) => Promise<string | { isNull: true } | null>
  form: (o: { title: string; description?: string; fields: FormField[]; submitText?: string }) => Promise<Record<string, string | null> | null>
}

export function createDialogs(setDlg: React.Dispatch<React.SetStateAction<PendingDialog | null>>): DialogsApi {
  let seq = 0
  const open = <T,>(base: Omit<PendingDialog, 'id' | 'resolve'>): Promise<T> =>
    new Promise<T>((resolve) => {
      const id = ++seq
      setDlg({
        ...base,
        id,
        resolve: (v) => {
          setDlg((cur) => (cur && cur.id === id ? null : cur))
          resolve(v as T)
        },
      })
    })
  return {
    confirm: (o) => open<boolean>({ kind: 'confirm', confirmText: 'Confirm', ...o }),
    prompt: (o) => open<string | null>({ kind: 'prompt', submitText: 'OK', ...o }),
    promptNullable: (o) => open<string | { isNull: true } | null>({ kind: 'prompt', submitText: 'OK', nullable: true, ...o }),
    form: (o) => open<Record<string, string | null> | null>({ kind: 'form', submitText: 'Submit', ...o }),
  }
}

export function DialogHost({ dlg }: { dlg: PendingDialog | null }) {
  if (!dlg) return null
  if (dlg.kind === 'confirm') return <ConfirmHost key={dlg.id} dlg={dlg} />
  if (dlg.kind === 'prompt') return <PromptHost key={dlg.id} dlg={dlg} />
  return <FormHost key={dlg.id} dlg={dlg} />
}

function ConfirmHost({ dlg }: { dlg: PendingDialog }) {
  return (
    <AlertDialog open onOpenChange={(open) => { if (!open) dlg.resolve(false) }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{dlg.title}</AlertDialogTitle>
          {dlg.description && <AlertDialogDescription>{dlg.description}</AlertDialogDescription>}
          {dlg.danger && <p className="text-[12px] font-medium text-destructive">This action cannot be undone.</p>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => dlg.resolve(false)}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => dlg.resolve(true)}
            className={dlg.danger ? 'bg-destructive text-destructive-foreground hover:bg-destructive/90' : undefined}
          >
            {dlg.confirmText ?? 'Confirm'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function PromptHost({ dlg }: { dlg: PendingDialog }) {
  const [value, setValue] = useState(dlg.defaultValue ?? '')
  return (
    <Dialog open onOpenChange={(open) => { if (!open) dlg.resolve(null) }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{dlg.title}</DialogTitle>
        </DialogHeader>
        {dlg.description && <p className="-mt-1 text-[12px] text-muted-foreground">{dlg.description}</p>}
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            dlg.resolve(value)
          }}
        >
          <Input autoFocus placeholder={dlg.placeholder} value={value} onChange={(e) => setValue(e.target.value)} />
          <div className="flex justify-end gap-1.5">
            <Button type="button" variant="ghost" onClick={() => dlg.resolve(null)}>
              Cancel
            </Button>
            {dlg.nullable && (
              <Button type="button" variant="secondary" onClick={() => dlg.resolve({ isNull: true })}>
                Set NULL
              </Button>
            )}
            <Button type="submit">{dlg.submitText ?? 'OK'}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function FormHost({ dlg }: { dlg: PendingDialog }) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [nulls, setNulls] = useState<Record<string, boolean>>({})
  return (
    <Dialog open onOpenChange={(open) => { if (!open) dlg.resolve(null) }}>
      <DialogContent className="max-h-[80vh] overflow-auto">
        <DialogHeader>
          <DialogTitle>{dlg.title}</DialogTitle>
        </DialogHeader>
        {dlg.description && <p className="-mt-1 text-[12px] text-muted-foreground">{dlg.description}</p>}
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            const out: Record<string, string | null> = {}
            for (const f of dlg.fields ?? []) out[f.key] = nulls[f.key] ? null : (values[f.key] ?? '')
            dlg.resolve(out)
          }}
        >
          {(dlg.fields ?? []).map((f) => (
            <label key={f.key} className="grid grid-cols-[140px_1fr_auto] items-center gap-2 text-[12px]">
              <Tip content={f.label}>
                <span className="truncate text-muted-foreground">{f.label}</span>
              </Tip>
              {f.multiline && f.type === 'password' ? <PasswordTextarea
                placeholder={f.placeholder}
                disabled={!!nulls[f.key]}
                value={nulls[f.key] ? 'NULL' : (values[f.key] ?? '')}
                onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
              /> : f.multiline ? <Textarea
                placeholder={f.placeholder}
                disabled={!!nulls[f.key]}
                value={nulls[f.key] ? 'NULL' : (values[f.key] ?? '')}
                onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
              /> : <Input
                type={f.type}
                placeholder={f.placeholder}
                disabled={!!nulls[f.key]}
                value={nulls[f.key] ? 'NULL' : (values[f.key] ?? '')}
                onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
              />}
              {f.allowNull ? (
                <Tip content={nulls[f.key] ? 'Unset NULL (edit a value)' : 'Set NULL'}>
                  <Button
                    type="button"
                    size="sm"
                    variant={nulls[f.key] ? 'secondary' : 'ghost'}
                    aria-pressed={!!nulls[f.key]}
                    aria-label={`Set ${f.label} to NULL`}
                    onClick={() => setNulls((n) => ({ ...n, [f.key]: !n[f.key] }))}
                  >
                    N
                  </Button>
                </Tip>
              ) : (
                <span />
              )}
            </label>
          ))}
          <div className="mt-1 flex justify-end gap-1.5">
            <Button type="button" variant="ghost" onClick={() => dlg.resolve(null)}>
              Cancel
            </Button>
            <Button type="submit">{dlg.submitText ?? 'Submit'}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
