import type { CommandId } from '@/commands/types'

export interface ShortcutOverrides {
  version: 1
  overrides: Partial<Record<CommandId, string[]>>
}

export type BindingMap = Record<CommandId, string[]>
