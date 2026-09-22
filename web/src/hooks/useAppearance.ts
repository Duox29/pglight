import { useLayoutEffect } from 'react'
import { applyAppearance, DEFAULT_APPEARANCE, normalizeAppearance, type AppearanceSettings } from '@/lib/appearance'
import { useAppPreference } from '@/lib/storage'

export function useAppearance() {
  const [appearance, setAppearance] = useAppPreference<AppearanceSettings>('appearance', DEFAULT_APPEARANCE)
  const normalized = normalizeAppearance(appearance)

  useLayoutEffect(() => {
    applyAppearance(normalized)
  }, [normalized])

  return { appearance: normalized, setAppearance }
}
