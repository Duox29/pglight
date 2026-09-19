import type { ReactElement } from 'react'
import { render, type RenderOptions } from '@testing-library/react'
import { TooltipProvider } from '../components/ui/tooltip'

/* Radix Tooltip (Tip) requires a provider; wrap all component renders. */
export function renderUi(ui: ReactElement, options?: RenderOptions) {
  return render(<TooltipProvider>{ui}</TooltipProvider>, options)
}
