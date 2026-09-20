import { describe, expect, it } from 'vitest'
import { matchesShortcut } from './matcher'
import { normalizeShortcut, shortcutFromEvent } from './normalize'

describe('shortcut normalization', () => {
  it('canonicalizes modifiers and aliases', () => {
    expect(normalizeShortcut('alt + shift + f')).toBe('Alt+Shift+F')
    expect(normalizeShortcut('Esc')).toBe('Escape')
    expect(normalizeShortcut('Ctrl+Ctrl+K')).toBeNull()
  })

  it('maps Mod to Ctrl on non-Mac platforms', () => {
    Object.defineProperty(navigator, 'platform', { value: 'Linux', configurable: true })
    expect(matchesShortcut(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true }), 'Mod+Enter')).toBe(true)
    expect(matchesShortcut(new KeyboardEvent('keydown', { key: 'Enter', metaKey: true }), 'Mod+Enter')).toBe(false)
  })

  it('maps Mod to Meta on macOS', () => {
    Object.defineProperty(navigator, 'platform', { value: 'MacIntel', configurable: true })
    expect(shortcutFromEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))).toBe('Meta+K')
    expect(matchesShortcut(new KeyboardEvent('keydown', { key: 'k', metaKey: true }), 'Mod+K')).toBe(true)
  })
})
