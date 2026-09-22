import { useEffect, useState } from 'react'
import { Pencil, Plus, RotateCcw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { Card } from './ui/card'
import { EmptyNote, ErrorText } from './ui/feedback'
import { ScrollArea } from './ui/scroll-area'
import { apiClient, type CompletionAlias } from '@/lib/api'
import { refreshAliases } from '@/lib/aliases'
import type { DialogsApi } from './dialogs'

/** Workspace tab owning completion aliases (ssf => SELECT * FROM). */
export function AliasesPanel({ dialogs }: { dialogs: DialogsApi }) {
  const [aliases, setAliases] = useState<CompletionAlias[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let stop = false
    const load = async () => {
      try {
        const j = await apiClient.listAliases()
        if (!stop) {
          setAliases(j.aliases ?? [])
          setError(j.error ?? '')
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

  const reload = async () => {
    try {
      const j = await apiClient.listAliases()
      setAliases(j.aliases ?? [])
      setError(j.error ?? '')
      refreshAliases()
    } catch (e) {
      setError(String(e))
    }
  }

  const addAlias = async () => {
    const v = await dialogs.form({
      title: 'New completion alias',
      description: 'Trigger fires on prefix match (e.g. ssf) and expands to the template.',
      fields: [
        { key: 'trigger', label: 'Trigger', placeholder: 'ssf' },
        { key: 'expansion', label: 'Expansion', placeholder: 'SELECT * FROM ' },
      ],
      submitText: 'Add',
    })
    if (!v) return
    try {
      const j = await apiClient.saveAlias(v['trigger'] ?? '', v['expansion'] ?? '')
      if (j.error) toast.error(j.error)
      else {
        toast.success(`Alias ${j.alias?.trigger} saved`)
        void reload()
      }
    } catch (e) {
      toast.error(String(e))
    }
  }

  const editAlias = async (a: CompletionAlias) => {
    const v = await dialogs.prompt({
      title: `Edit alias ${a.trigger}`,
      description: 'Builtin triggers revert only via Reset — editing creates an override.',
      defaultValue: a.expansion,
      submitText: 'Save',
    })
    if (v == null) return
    try {
      const j = await apiClient.saveAlias(a.trigger, v)
      if (j.error) toast.error(j.error)
      else {
        toast.success(`Alias ${a.trigger} saved`)
        void reload()
      }
    } catch (e) {
      toast.error(String(e))
    }
  }

  const deleteAlias = async (trigger: string) => {
    const ok = await dialogs.confirm({
      title: `Delete alias ${trigger}?`,
      description: 'Builtin triggers revert to their default expansion.',
      confirmText: 'Delete',
      danger: true,
    })
    if (!ok) return
    try {
      const j = await apiClient.deleteAlias(trigger)
      if (j.error) toast.error(j.error)
      else {
        toast.success(`Alias ${trigger} deleted`)
        void reload()
      }
    } catch (e) {
      toast.error(String(e))
    }
  }

  const resetAliases = async () => {
    const ok = await dialogs.confirm({
      title: 'Reset aliases to defaults?',
      description: 'All custom entries and builtin overrides are dropped.',
      confirmText: 'Reset',
      danger: true,
    })
    if (!ok) return
    try {
      const j = await apiClient.resetAliases()
      if (j.error) toast.error(j.error)
      else {
        toast.success('Aliases reset to defaults')
        void reload()
      }
    } catch (e) {
      toast.error(String(e))
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <p className="text-[11px] text-muted-foreground">Short triggers expand on prefix match — e.g. ssf → SELECT * FROM.</p>
        <div className="flex shrink-0 gap-1">
          <Tip content="Add alias">
            <span className="inline-flex">
              <Button size="sm" variant="ghost" onClick={addAlias} aria-label="Add alias">
                <Plus />
              </Button>
            </span>
          </Tip>
          <Tip content="Reset to defaults">
            <span className="inline-flex">
              <Button size="sm" variant="ghost" onClick={resetAliases} aria-label="Reset aliases">
                <RotateCcw />
              </Button>
            </span>
          </Tip>
        </div>
      </div>
      <ErrorText message={error} />
      <ScrollArea className="min-h-0 flex-1 pr-1">
        <div className="space-y-1.5">
          {!aliases.length && !error ? (
            <EmptyNote text="Loading…" />
          ) : (
            aliases.map((a) => (
              <Card key={a.trigger} className="p-2">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 text-[12px]">
                      <b className="font-mono">{a.trigger}</b>
                      {a.builtin ? (
                        <span className="rounded bg-muted px-1 text-[10px] text-muted-foreground">builtin</span>
                      ) : (
                        <span className="rounded bg-muted px-1 text-[10px] text-muted-foreground">custom</span>
                      )}
                    </div>
                    <pre className="mt-1 max-h-[120px] overflow-auto whitespace-pre-wrap rounded border border-input bg-background px-2 py-1 font-mono text-[11px]">{a.expansion}</pre>
                  </div>
                  <div className="flex shrink-0 gap-0.5">
                    <Tip content={a.builtin ? 'Override builtin' : 'Edit alias'}>
                      <span className="inline-flex">
                        <Button size="sm" variant="ghost" onClick={() => editAlias(a)} aria-label={`Edit alias ${a.trigger}`}>
                          <Pencil />
                        </Button>
                      </span>
                    </Tip>
                    {!a.builtin && (
                      <Tip content="Delete alias">
                        <span className="inline-flex">
                          <Button size="sm" variant="ghost" onClick={() => deleteAlias(a.trigger)} aria-label={`Delete alias ${a.trigger}`}>
                            <Trash2 />
                          </Button>
                        </span>
                      </Tip>
                    )}
                  </div>
                </div>
              </Card>
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  )
}
