export type CommandId =
  | 'palette.open'
  | 'query.new'
  | 'query.run'
  | 'query.complete'
  | 'query.cancel'
  | 'query.format'
  | 'query.explain'
  | 'query.clearResults'
  | 'query.saveSnippet'
  | 'tab.close'
  | 'tab.closeOthers'
  | 'tab.closeLeft'
  | 'tab.closeRight'
  | 'tab.closeAll'
  | 'tab.next'
  | 'tab.previous'
  | 'tab.activate.1'
  | 'tab.activate.2'
  | 'tab.activate.3'
  | 'tab.activate.4'
  | 'tab.activate.5'
  | 'tab.activate.6'
  | 'tab.activate.7'
  | 'tab.activate.8'
  | 'tab.activate.9'
  | 'table.refresh'
  | 'table.nextPage'
  | 'table.previousPage'
  | 'workspace.history'
  | 'workspace.snippets'
  | 'workspace.dashboard'
  | 'workspace.settings'
  | 'workspace.shortcuts'
  | 'workspace.docs'
  | 'split.right'
  | 'split.down'
  | 'split.swap'
  | 'split.close'
  | 'explorer.refresh'
  | 'session.connect'
  | 'session.disconnect'

export type CommandScope = 'global' | 'query' | 'table' | 'erd' | 'object' | 'dialog'

export interface CommandDefinition {
  id: CommandId
  title: string
  category: string
  description?: string
  allowInInput?: boolean
  allowInDialog?: boolean
  destructive?: boolean
}

export interface CommandRegistrationOptions {
  scope?: CommandScope
  enabled?: boolean | (() => boolean)
  priority?: number
}

export type CommandHandler = () => void | Promise<void>
