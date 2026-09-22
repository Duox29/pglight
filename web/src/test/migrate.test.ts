import { describe, expect, it } from 'vitest'
import { migrateShortcutSettings } from '../shortcuts/migrate'

describe('shortcut settings migration', () => {
  it('renames the legacy search command and caps bindings', () => {
    expect(migrateShortcutSettings({ version: 0, bindings: { 'search.open': ['Ctrl+K'], 'query.run': ['F9', 'Mod+Enter', 'Alt+R', 'Alt+E', 'Alt+T'] } })).toEqual({
      version: 1,
      overrides: { 'palette.open': ['Ctrl+K'], 'query.run': ['F9', 'Mod+Enter', 'Alt+R', 'Alt+E'] },
    })
  })
})
