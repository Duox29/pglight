import type { CommandId } from '@/commands/types'
import type { BindingMap } from './types'

export const DEFAULT_BINDINGS: Partial<Record<CommandId, string[]>> = {
  'palette.open': ['Mod+K'],
  'query.new': ['Mod+Alt+N'],
  'query.run': ['Mod+Enter'],
  'query.complete': ['Mod+Space'],
  'query.cancel': ['Escape'],
  'query.format': ['Shift+Alt+F'],
  'tab.close': ['Mod+W'],
  'tab.next': ['Ctrl+Tab'],
  'tab.previous': ['Ctrl+Shift+Tab'],
  'tab.activate.1': ['Alt+1'],
  'tab.activate.2': ['Alt+2'],
  'tab.activate.3': ['Alt+3'],
  'tab.activate.4': ['Alt+4'],
  'tab.activate.5': ['Alt+5'],
  'tab.activate.6': ['Alt+6'],
  'tab.activate.7': ['Alt+7'],
  'tab.activate.8': ['Alt+8'],
  'tab.activate.9': ['Alt+9'],
}

export function effectiveBindings(overrides: Partial<Record<CommandId, string[]>>): BindingMap {
  return Object.fromEntries(Object.keys(overrides).length === 0 ? Object.entries(DEFAULT_BINDINGS) : Object.entries({ ...DEFAULT_BINDINGS, ...overrides })) as BindingMap
}
