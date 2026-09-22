import { beforeEach, describe, expect, it } from 'vitest'
import { applyAppearance, DEFAULT_APPEARANCE, normalizeAppearance, presetAppearance } from '../lib/appearance'

describe('appearance settings', () => {
  beforeEach(() => {
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.className = ''
    document.documentElement.removeAttribute('style')
  })

  it('provides dark and light presets', () => {
    expect(presetAppearance('dark').mode).toBe('dark')
    expect(presetAppearance('light').mode).toBe('light')
    expect(presetAppearance('ocean').preset).toBe('ocean')
  })

  it('falls back to the default for invalid stored data', () => {
    expect(normalizeAppearance({ version: 1, mode: 'neon' })).toBe(DEFAULT_APPEARANCE)
  })

  it('applies semantic CSS variables and the dark class', () => {
    applyAppearance(DEFAULT_APPEARANCE)
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.documentElement.style.getPropertyValue('--background')).toMatch(/^\d+ \d+% \d+%$/)
    expect(document.documentElement.style.getPropertyValue('--primary')).toMatch(/^\d+ \d+% \d+%$/)
  })

  it('removes the dark class for light mode', () => {
    applyAppearance(presetAppearance('light'))
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
