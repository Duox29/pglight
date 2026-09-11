import { useEffect, useRef } from 'react'
import { EditorState, Prec } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { autocompletion, closeBrackets, completionKeymap, pickedCompletion, startCompletion } from '@codemirror/autocomplete'
import { PostgreSQL, sql } from '@codemirror/lang-sql'
import { ensureSnapshot } from '@/lib/schemaCache'
import { createCompleteSource, recordUse } from '@/lib/complete'

/** Selection access for Run-selection without touching editor internals. */
export interface SqlEditorHandle {
  getSelection: () => string
}

interface Props {
  value: string
  session: string
  onChange: (v: string) => void
  /** Ctrl/Cmd+Enter — same contract as the old Textarea key handler. */
  onCtrlEnter: () => void
  handleRef: React.MutableRefObject<SqlEditorHandle | null>
}

/**
 * CodeMirror 6 SQL editor (PostgreSQL dialect) replacing the plain Textarea.
 * Completion reads the in-memory schema snapshot — zero network per keystroke.
 * Layout contract: fills the parent flex column (same flex-1/min-h-0 slot the
 * Textarea occupied) so the vertical ResizablePanel split keeps working.
 */
export function SqlEditor({ value, session, onChange, onCtrlEnter, handleRef }: Props) {
  const mountRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const cb = useRef({ onChange, onCtrlEnter })
  useEffect(() => {
    cb.current = { onChange, onCtrlEnter }
  })
  // First-render SQL: later prop changes sync via the value effect below.
  const initialRef = useRef(value)

  useEffect(() => {
    const mount = mountRef.current
    if (!mount) return
    void ensureSnapshot(session)
    const src = createCompleteSource(session)
    const update = EditorView.updateListener.of((u) => {
      if (u.docChanged) cb.current.onChange(u.state.doc.toString())
      const picked = u.transactions.map((t) => t.annotation(pickedCompletion)).find((a) => a)
      if (picked) recordUse(picked.label)
    })
    const state = EditorState.create({
      // Initial doc only — later prop changes sync via the effect below.
      doc: initialRef.current,
      extensions: [
        history(),
        sql({ dialect: PostgreSQL }),
        closeBrackets(),
        EditorView.lineWrapping,
        autocompletion({ override: [src], activateOnTyping: true, maxRenderedOptions: 50 }),
        Prec.high(
          keymap.of([
            { key: 'Ctrl-Enter', mac: 'Cmd-Enter', run: () => (cb.current.onCtrlEnter(), true) },
            { key: 'Ctrl-Space', mac: 'Cmd-Space', run: startCompletion },
          ]),
        ),
        keymap.of([...completionKeymap, ...defaultKeymap, ...historyKeymap]),
        update,
        EditorView.theme({
          '&': { height: '100%', fontSize: '12.5px', backgroundColor: 'hsl(var(--background))', color: 'hsl(var(--foreground))' },
          '.cm-content': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', caretColor: 'hsl(var(--foreground))', padding: '8px 0' },
          '.cm-line': { padding: '0 8px' },
          '.cm-gutters': { display: 'none' },
          '.cm-focused': { outline: 'none' },
          '.cm-tooltip.cm-tooltip-autocomplete': {
            backgroundColor: 'hsl(var(--popover))',
            color: 'hsl(var(--popover-foreground))',
            border: '1px solid hsl(var(--border))',
            borderRadius: '6px',
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
            fontSize: '12px',
          },
          '.cm-tooltip-autocomplete > ul': { maxHeight: '240px' },
          '.cm-tooltip-autocomplete .cm-completionDetail': { color: 'hsl(var(--muted-foreground))', fontStyle: 'normal' },
          'li[aria-selected]': { backgroundColor: 'hsl(var(--accent))', color: 'hsl(var(--accent-foreground))' },
        }),
      ],
    })
    const view = new EditorView({ state, parent: mount })
    viewRef.current = view
    handleRef.current = {
      getSelection: () => {
        const v = viewRef.current
        if (!v) return ''
        const r = v.state.selection.main
        return r.empty ? '' : v.state.sliceDoc(r.from, r.to)
      },
    }
    return () => {
      view.destroy()
      viewRef.current = null
      handleRef.current = null
    }
    // Session-scoped source: a new session means a new editor (rare).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session])

  // Controlled value: Format button / tab restore set props; push into the
  // editor without disturbing the caret when the text is already equal.
  useEffect(() => {
    const v = viewRef.current
    if (!v) return
    const cur = v.state.doc.toString()
    if (cur === value) return
    v.dispatch({
      changes: { from: 0, to: cur.length, insert: value },
      selection: { anchor: Math.min(v.state.selection.main.head, value.length) },
    })
  }, [value])

  return <div ref={mountRef} className="min-h-0 flex-1 overflow-hidden rounded-md border border-input bg-background" aria-label="SQL editor" />;
}
