import { RotateCcw } from 'lucide-react'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { ColorPicker } from './ui/color-picker'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { presetAppearance, type AppearanceColors } from '@/lib/appearance'
import { useAppearance } from '@/hooks/useAppearance'

export function AppearancePanel() {
  const { appearance, setAppearance } = useAppearance()

  const updateColor = (key: keyof AppearanceColors, value: string) => {
    setAppearance({
      ...appearance,
      preset: 'custom',
      colors: { ...appearance.colors, [key]: value },
    })
  }

  const selectPreset = (preset: string) => {
    if (preset === 'custom') {
      setAppearance({ ...appearance, preset: 'custom' })
      return
    }
    if (preset !== 'dark' && preset !== 'light' && preset !== 'ocean') return
    setAppearance(presetAppearance(preset))
  }

  const selectMode = (mode: 'dark' | 'light') => {
    setAppearance(presetAppearance(mode))
  }

  return (
    <div className="flex flex-col gap-2">
      <Card className="p-2.5">
        <div className="mb-1 text-[12px] font-semibold">Appearance</div>
        <p className="mb-2 text-[11px] text-muted-foreground">Changes apply immediately across the app.</p>
        <div className="flex flex-col gap-2 text-[12px]">
          <label className="flex items-center justify-between gap-2">
            <span>Theme</span>
            <Select value={appearance.mode} onValueChange={(value) => selectMode(value as 'dark' | 'light')}>
              <SelectTrigger className="w-[120px]" aria-label="Theme">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="dark">Dark</SelectItem>
                <SelectItem value="light">Light</SelectItem>
              </SelectContent>
            </Select>
          </label>
          <label className="flex items-center justify-between gap-2">
            <span>Preset</span>
            <Select value={appearance.preset} onValueChange={selectPreset}>
              <SelectTrigger className="w-[120px]" aria-label="Appearance preset">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="dark">Pglight Dark</SelectItem>
                <SelectItem value="light">Pglight Light</SelectItem>
                <SelectItem value="ocean">Ocean Blue</SelectItem>
                <SelectItem value="custom">Custom</SelectItem>
              </SelectContent>
            </Select>
          </label>
          <div className="grid grid-cols-[1fr_auto] items-center gap-x-2 gap-y-1.5">
            {([
              ['background', 'Background'],
              ['foreground', 'Text'],
              ['panel', 'Panel'],
              ['accent', 'Accent'],
              ['border', 'Border'],
            ] as const).map(([key, label]) => (
              <div key={key} className="contents">
                <span>{label}</span>
                <ColorPicker value={appearance.colors[key]} onChange={(value) => updateColor(key, value)} ariaLabel={`${label} color`} />
              </div>
            ))}
          </div>
          <div
            className="rounded-md border p-2 text-[11px]"
            style={{ backgroundColor: appearance.colors.background, color: appearance.colors.foreground, borderColor: appearance.colors.border }}
          >
            <div className="mb-1 font-semibold">Preview</div>
            <div className="rounded border p-1.5" style={{ backgroundColor: appearance.colors.panel, borderColor: appearance.colors.border }}>
              <span>Workspace text</span>
              <span className="ml-2 rounded px-1.5 py-0.5" style={{ backgroundColor: appearance.colors.accent, color: '#fff' }}>Accent</span>
            </div>
          </div>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => setAppearance(presetAppearance(appearance.mode))}
          >
            <RotateCcw /> Reset {appearance.mode === 'dark' ? 'Dark' : 'Light'}
          </Button>
        </div>
      </Card>
    </div>
  )
}
