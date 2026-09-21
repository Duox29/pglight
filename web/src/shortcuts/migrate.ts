import type { CommandId } from '@/commands/types'

interface LegacyShortcutSettings {
  version?: number
  overrides?: Record<string, string[]>
  bindings?: Record<string, string[]>
}

const RENAMED: Record<string, CommandId> = { 'search.open': 'palette.open', 'query.explainAnalyze': 'query.explain' }

export function migrateShortcutSettings(input: LegacyShortcutSettings | null | undefined): { version: 1; overrides: Partial<Record<CommandId, string[]>> } {
  const source = input?.overrides ?? input?.bindings ?? {}
  const overrides: Partial<Record<CommandId, string[]>> = {}
  for (const [rawId, values] of Object.entries(source)) {
    if (rawId === 'query.explainAnalyze' && Object.prototype.hasOwnProperty.call(source, 'query.explain')) continue
    const id = RENAMED[rawId] ?? rawId
    if (Array.isArray(values) && values.every((value) => typeof value === 'string')) overrides[id as CommandId] = values.slice(0, 4)
  }
  return { version: 1, overrides }
}
