import { useEffect, useRef } from 'react'
import { EditorState, Prec, RangeSet, StateEffect, StateField } from '@codemirror/state'
import { Decoration, EditorView, keymap } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { autocompletion, closeBrackets, completionKeymap, pickedCompletion, startCompletion } from '@codemirror/autocomplete'
import { PostgreSQL, sql } from '@codemirror/lang-sql'
import { ensureSnapshot } from '@/lib/schemaCache'
import { createCompleteSource, recordUse } from '@/lib/complete'
/** Error jump + blink + selection access without touching editor internals. */
export interface SqlEditorHandle {
  getSelection: () => string
  gotoLine: (line: number, column?: number) => void
  flashErrorLine: (line: number, column?: number) => void
  clearErrorFlash: () => void
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
    // One error line at a time: a StateField holding a RangeSet of a single
    // line decoration. Edits remap it; only explicit set/clear effects move it.
    const setErrLine = StateEffect.define<number | null>()
    const errLineField = StateField.define<RangeSet<Decoration>>({
      create: () => RangeSet.empty,
      update: (set, tr) => {
        set = set.map(tr.changes)
        for (const e of tr.effects) {
          if (e.is(setErrLine)) {
            if (e.value == null) return RangeSet.empty
            const n = Math.min(Math.max(1, e.value), tr.state.doc.lines)
            const line = tr.state.doc.line(n)
            set = RangeSet.of([{ from: line.from, to: line.from, value: Decoration.line({ class: 'cm-errline' }) }])
          }
        }
        return set
      },
      provide: (f) => EditorView.decorations.from(f),
    })
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
        errLineField,
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
          '.cm-errline': { backgroundColor: 'hsl(var(--destructive) / 0.22)' },
          '@keyframes pglight-errblink': { '0%,100%': { backgroundColor: 'hsl(var(--destructive) / 0.22)' }, '50%': { backgroundColor: 'hsl(var(--destructive) / 0.55)' } },
          '.cm-errline-blink': { animation: 'pglight-errblink 0.45s ease-in-out 3' },
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
      gotoLine: (line: number, column?: number) => {
        const v = viewRef.current
        if (!v || line < 1) return
        const docLine = v.state.doc.line(Math.min(line, v.state.doc.lines))
        const col = Math.max(1, Math.min(column ?? 1, docLine.length + 1))
        const pos = docLine.from + (col - 1)
        v.dispatch({ selection: { anchor: pos }, scrollIntoView: true })
        v.focus()
      },
      // Steady highlight + 3 CSS blinks (~1.35s), then the line stays
      // tinted until the next run/clear/tab switch. Decoration remaps on
      // edit; re-running re-flashes from the current line.
      flashErrorLine: (line: number, column?: number) => {
        const v = viewRef.current
        if (!v || line < 1) return
        const n = Math.min(line, v.state.doc.lines)
        const docLine = v.state.doc.line(n)
        const col = Math.max(1, Math.min(column ?? 1, docLine.length + 1))
        const pos = docLine.from + (col - 1)
        v.dispatch({ effects: setErrLine.of(n), selection: { anchor: pos }, scrollIntoView: true })
        v.focus()
        const el = v.contentDOM.querySelector('.cm-errline')
        if (el) {
          el.classList.remove('cm-errline-blink')
          // Forced reflow restarts the animation when the same line fails twice.
          void (el as HTMLElement).offsetWidth
          el.classList.add('cm-errline-blink')
        }
      },
      clearErrorFlash: () => {
        viewRef.current?.dispatch({ effects: setErrLine.of(null) })
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
