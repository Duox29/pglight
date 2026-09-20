import type { CommandDefinition } from '@/commands/types'
import { normalizeShortcut, shortcutFromEvent } from './normalize'

export function matchesShortcut(event: KeyboardEvent, binding: string): boolean {
  const actual = shortcutFromEvent(event)
  const expected = normalizeShortcut(binding)
  if (!actual || !expected) return false
  if (expected.includes('Mod+')) {
    const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
    return actual === expected.replace('Mod', isMac ? 'Meta' : 'Ctrl')
  }
  return actual === expected
}

export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.closest('.cm-editor')) return true
  const tag = target.tagName.toLowerCase()
  return tag === 'input' || tag === 'textarea' || tag === 'select' || target.isContentEditable
}

export function canDispatch(definition: CommandDefinition, event: KeyboardEvent): boolean {
  if (event.target instanceof HTMLElement && event.target.closest('[role="dialog"]')) return definition.allowInDialog === true
  if (!isEditableTarget(event.target)) return true
  return definition.allowInInput === true
}
