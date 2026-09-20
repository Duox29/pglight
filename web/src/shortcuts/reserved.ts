const RESERVED = new Set([
  'Ctrl+L', 'Ctrl+R', 'Ctrl+T', 'Ctrl+N', 'Ctrl+W', 'Ctrl+Tab', 'Ctrl+Shift+Tab', 'Ctrl+Shift+T',
  'Meta+Q', 'Meta+W', 'Meta+N', 'Meta+L',
])

export function isReservedShortcut(binding: string): boolean {
  return RESERVED.has(binding) || RESERVED.has(binding.replace('Mod', 'Ctrl')) || RESERVED.has(binding.replace('Mod', 'Meta'))
}
