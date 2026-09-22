import { afterEach, describe, expect, it } from 'vitest'
import { matchesShortcut } from '../shortcuts/matcher'
import { normalizeShortcut, shortcutFromEvent, toCodeMirrorKey } from '../shortcuts/normalize'
import { isReservedShortcut } from '../shortcuts/reserved'

const originalPlatform = navigator.platform

afterEach(() => {
  Object.defineProperty(navigator, 'platform', { value: originalPlatform, configurable: true })
})

describe('shortcut normalization', () => {
  it('canonicalizes modifiers and aliases', () => {
    expect(normalizeShortcut('alt + shift + f')).toBe('Alt+Shift+F')
    expect(normalizeShortcut('Esc')).toBe('Escape')
    expect(normalizeShortcut('Ctrl+Ctrl+K')).toBeNull()
    expect(normalizeShortcut('Shift+Enter+F')).toBeNull()
    expect(normalizeShortcut('Alt+')).toBeNull()
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

  it('generates CodeMirror platform keymaps and detects browser reservations', () => {
    expect(toCodeMirrorKey('Mod+Enter')).toEqual({ key: 'Ctrl-Enter', mac: 'Cmd-Enter' })
    expect(toCodeMirrorKey('Escape')).toEqual({ key: 'Escape', mac: undefined })
    expect(isReservedShortcut('Mod+N')).toBe(true)
    expect(isReservedShortcut('Mod+W')).toBe(true)
    expect(isReservedShortcut('F9')).toBe(false)
  })
})
