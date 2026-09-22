import type { CommandDefinition, CommandId } from './types'

const defs: CommandDefinition[] = [
  { id: 'palette.open', title: 'Command Palette', category: 'General', allowInInput: true, allowInDialog: true },
  { id: 'query.new', title: 'New Query', category: 'Query', allowInInput: true },
  { id: 'query.run', title: 'Run Query', category: 'Query' },
  { id: 'query.complete', title: 'Autocomplete SQL', category: 'Query' },
  { id: 'query.cancel', title: 'Cancel Running Query', category: 'Query', allowInInput: true },
  { id: 'query.format', title: 'Format SQL', category: 'Query' },
  { id: 'query.explain', title: 'Explain', category: 'Query', allowInInput: true },
  { id: 'query.clearResults', title: 'Clear Query Results', category: 'Query' },
  { id: 'query.saveSnippet', title: 'Save Query as Snippet', category: 'Query' },
  { id: 'tab.close', title: 'Close Tab', category: 'Tabs', allowInInput: true },
  { id: 'tab.closeOthers', title: 'Close Other Tabs', category: 'Tabs' },
  { id: 'tab.closeLeft', title: 'Close Tabs to the Left', category: 'Tabs' },
  { id: 'tab.closeRight', title: 'Close Tabs to the Right', category: 'Tabs' },
  { id: 'tab.closeAll', title: 'Close All Tabs', category: 'Tabs' },
  { id: 'tab.next', title: 'Next Tab', category: 'Tabs', allowInInput: true },
  { id: 'tab.previous', title: 'Previous Tab', category: 'Tabs', allowInInput: true },
  ...Array.from({ length: 9 }, (_, i) => ({ id: `tab.activate.${i + 1}` as CommandId, title: `Activate Tab ${i + 1}`, category: 'Tabs', allowInInput: true })),
  { id: 'table.refresh', title: 'Refresh Table', category: 'Table' },
  { id: 'table.nextPage', title: 'Next Table Page', category: 'Table' },
  { id: 'table.previousPage', title: 'Previous Table Page', category: 'Table' },
  { id: 'workspace.history', title: 'Open Query History', category: 'Workspace' },
  { id: 'workspace.snippets', title: 'Open Snippets', category: 'Workspace' },
  { id: 'workspace.dashboard', title: 'Open Dashboard', category: 'Workspace' },
  { id: 'workspace.appearance', title: 'Open Appearance', category: 'Workspace' },
  { id: 'workspace.settings', title: 'Open Settings', category: 'Workspace' },
  { id: 'workspace.shortcuts', title: 'Open Keyboard Shortcuts', category: 'Workspace' },
  { id: 'workspace.docs', title: 'Open Documentation', category: 'Workspace' },
  { id: 'split.right', title: 'Split Workspace Right', category: 'Workspace' },
  { id: 'split.down', title: 'Split Workspace Down', category: 'Workspace' },
  { id: 'split.swap', title: 'Swap Workspace Panes', category: 'Workspace' },
  { id: 'split.close', title: 'Close Workspace Split', category: 'Workspace' },
  { id: 'explorer.refresh', title: 'Refresh Explorer', category: 'Explorer' },
  { id: 'session.connect', title: 'Connect to Database', category: 'Session' },
  { id: 'session.disconnect', title: 'Disconnect Database', category: 'Session' },
]

export const COMMANDS = defs
export const COMMAND_BY_ID = new Map(defs.map((definition) => [definition.id, definition]))

export function getCommand(id: CommandId): CommandDefinition {
  return COMMAND_BY_ID.get(id) ?? { id, title: id, category: 'Other' }
}
