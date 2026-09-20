const MODIFIERS = new Set(['Mod', 'Ctrl', 'Alt', 'Shift', 'Meta'])
const KEY_ALIASES: Record<string, string> = {
  Esc: 'Escape',
  Spacebar: 'Space',
  ' ': 'Space',
  Return: 'Enter',
  Del: 'Delete',
}

export function normalizeKey(key: string): string {
  const trimmed = key.trim()
  if (!trimmed) return ''
  const alias = KEY_ALIASES[trimmed]
  if (alias) return alias
  if (trimmed.length === 1) return trimmed.toUpperCase()
  return trimmed[0].toUpperCase() + trimmed.slice(1)
}

export function normalizeShortcut(value: string): string | null {
  const parts = value.split('+').map((part) => part.trim()).filter(Boolean)
  if (!parts.length) return null
  const modifiers = new Set<string>()
  let key = ''
  for (const part of parts) {
    const normalized = normalizeKey(part)
    if (MODIFIERS.has(normalized)) {
      if (modifiers.has(normalized)) return null
      modifiers.add(normalized)
    } else {
      if (key) return null
      key = normalized
    }
  }
  if (!key || key === 'Mod' || modifiers.has(key)) return null
  const ordered = ['Mod', 'Ctrl', 'Meta', 'Alt', 'Shift'].filter((modifier) => modifiers.has(modifier))
  return [...ordered, key].join('+')
}

/** Canonical form for comparing a physical platform modifier with logical Mod. */
export function shortcutComparisonKey(value: string): string | null {
  const normalized = normalizeShortcut(value)
  if (!normalized) return null
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
  return normalized.replace('Mod', isMac ? 'Meta' : 'Ctrl')
}

export function shortcutFromEvent(event: KeyboardEvent): string | null {
  const key = normalizeKey(event.key)
  if (!key || ['Control', 'Meta', 'Alt', 'Shift'].includes(key)) return null
  const modifiers: string[] = []
  if (event.ctrlKey) modifiers.push('Ctrl')
  if (event.metaKey) modifiers.push('Meta')
  if (event.altKey) modifiers.push('Alt')
  if (event.shiftKey) modifiers.push('Shift')
  return normalizeShortcut([...modifiers, key].join('+'))
}

export function shortcutForDisplay(binding: string, platform = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? 'mac' : 'other'): string {
  return binding
    .split('Mod').join(platform === 'mac' ? '⌘' : 'Ctrl')
    .split('Ctrl').join('Ctrl')
    .split('Alt').join(platform === 'mac' ? '⌥' : 'Alt')
    .split('Shift').join(platform === 'mac' ? '⇧' : 'Shift')
    .split('Meta').join('⌘')
    .split('+').join(platform === 'mac' ? '' : '+')
}

export function toCodeMirrorKey(binding: string): { key: string; mac?: string } | null {
  const normalized = normalizeShortcut(binding)
  if (!normalized) return null
  const parts = normalized.split('+')
  const key = parts.pop() ?? ''
  const modifiers = parts.filter((part) => part !== 'Meta')
  const base = [...modifiers.map((part) => part === 'Mod' ? 'Ctrl' : part), key].join('-')
  const mac = [...modifiers.map((part) => part === 'Mod' ? 'Cmd' : part), key].join('-')
  return { key: base, mac: mac === base ? undefined : mac }
}
