export type AppearanceMode = 'dark' | 'light'
export type AppearancePreset = 'dark' | 'light' | 'ocean' | 'custom'

export interface AppearanceColors {
  background: string
  foreground: string
  panel: string
  accent: string
  border: string
}

export interface AppearanceSettings {
  version: 1
  mode: AppearanceMode
  preset: AppearancePreset
  colors: AppearanceColors
}

export const APPEARANCE_DEFAULTS: Record<Exclude<AppearancePreset, 'custom'>, AppearanceSettings> = {
  dark: {
    version: 1,
    mode: 'dark',
    preset: 'dark',
    colors: {
      background: '#0d1320',
      foreground: '#e4e9f0',
      panel: '#121925',
      accent: '#2e8bff',
      border: '#2b3444',
    },
  },
  light: {
    version: 1,
    mode: 'light',
    preset: 'light',
    colors: {
      background: '#f7f9fc',
      foreground: '#172033',
      panel: '#ffffff',
      accent: '#2563eb',
      border: '#d7deea',
    },
  },
  ocean: {
    version: 1,
    mode: 'dark',
    preset: 'ocean',
    colors: {
      background: '#071a2b',
      foreground: '#dff4ff',
      panel: '#0b2740',
      accent: '#22b8ff',
      border: '#1e4b66',
    },
  },
}

export const DEFAULT_APPEARANCE = APPEARANCE_DEFAULTS.dark

const COLOR_KEYS: (keyof AppearanceColors)[] = ['background', 'foreground', 'panel', 'accent', 'border']
const HEX_COLOR = /^#[0-9a-f]{6}$/i

export function isAppearanceSettings(value: unknown): value is AppearanceSettings {
  if (!value || typeof value !== 'object') return false
  const v = value as Partial<AppearanceSettings>
  if (v.version !== 1 || (v.mode !== 'dark' && v.mode !== 'light')) return false
  if (v.preset !== 'dark' && v.preset !== 'light' && v.preset !== 'ocean' && v.preset !== 'custom') return false
  if (!v.colors || typeof v.colors !== 'object') return false
  return COLOR_KEYS.every((key) => HEX_COLOR.test((v.colors as Partial<AppearanceColors>)[key] ?? ''))
}

export function normalizeAppearance(value: unknown): AppearanceSettings {
  return isAppearanceSettings(value) ? value : DEFAULT_APPEARANCE
}

export function presetAppearance(preset: Exclude<AppearancePreset, 'custom'>): AppearanceSettings {
  return APPEARANCE_DEFAULTS[preset]
}

function hexToRgb(hex: string) {
  return {
    r: Number.parseInt(hex.slice(1, 3), 16),
    g: Number.parseInt(hex.slice(3, 5), 16),
    b: Number.parseInt(hex.slice(5, 7), 16),
  }
}

function hexToHsl(hex: string): string {
  const { r: rawR, g: rawG, b: rawB } = hexToRgb(hex)
  const r = rawR / 255
  const g = rawG / 255
  const b = rawB / 255
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const delta = max - min
  let h = 0
  const l = (max + min) / 2
  const s = delta === 0 ? 0 : delta / (1 - Math.abs(2 * l - 1))

  if (delta !== 0) {
    if (max === r) h = 60 * (((g - b) / delta) % 6)
    else if (max === g) h = 60 * ((b - r) / delta + 2)
    else h = 60 * ((r - g) / delta + 4)
  }
  if (h < 0) h += 360
  return `${Math.round(h)} ${Math.round(s * 100)}% ${Math.round(l * 100)}%`
}

function mixHex(first: string, second: string, secondWeight: number): string {
  const a = hexToRgb(first)
  const b = hexToRgb(second)
  const weight = Math.max(0, Math.min(1, secondWeight))
  const channel = (left: number, right: number) => Math.round(left * (1 - weight) + right * weight).toString(16).padStart(2, '0')
  return `#${channel(a.r, b.r)}${channel(a.g, b.g)}${channel(a.b, b.b)}`
}

function luminance(hex: string): number {
  return Object.values(hexToRgb(hex)).reduce((sum, channel, index) => {
    const value = channel / 255
    const linear = value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
    return sum + linear * [0.2126, 0.7152, 0.0722][index]
  }, 0)
}

function readableOn(hex: string): string {
  return luminance(hex) > 0.48 ? '#172033' : '#ffffff'
}

/** Applies the semantic palette to the document and all Tailwind tokens. */
export function applyAppearance(settings: AppearanceSettings): void {
  const root = document.documentElement
  const { background, foreground, panel, accent, border } = settings.colors
  const muted = mixHex(foreground, background, 0.46)
  const secondary = mixHex(panel, background, 0.55)
  const accentSurface = mixHex(background, accent, 0.2)

  root.dataset.theme = settings.mode
  root.classList.toggle('dark', settings.mode === 'dark')
  const vars: Record<string, string> = {
    '--background': hexToHsl(background),
    '--foreground': hexToHsl(foreground),
    '--card': hexToHsl(panel),
    '--card-foreground': hexToHsl(foreground),
    '--popover': hexToHsl(panel),
    '--popover-foreground': hexToHsl(foreground),
    '--primary': hexToHsl(accent),
    '--primary-foreground': hexToHsl(readableOn(accent)),
    '--secondary': hexToHsl(secondary),
    '--secondary-foreground': hexToHsl(foreground),
    '--muted': hexToHsl(secondary),
    '--muted-foreground': hexToHsl(muted),
    '--accent': hexToHsl(accentSurface),
    '--accent-foreground': hexToHsl(foreground),
    '--border': hexToHsl(border),
    '--input': hexToHsl(border),
    '--ring': hexToHsl(accent),
  }
  for (const [name, value] of Object.entries(vars)) root.style.setProperty(name, value)
}
