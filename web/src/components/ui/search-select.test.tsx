import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderUi } from '../../test/utils'
import { SearchSelect, type SearchSelectOption } from './search-select'

const options: SearchSelectOption[] = [
  { value: '0', label: 'public.companies.id' },
  { value: '1', label: 'public.orders.company_id' },
  { value: '2', label: 'sales.regions.code' },
]

function renderSelect(over: Partial<React.ComponentProps<typeof SearchSelect>> = {}) {
  const onChange = vi.fn()
  renderUi(<SearchSelect value="" options={options} placeholder="Pick source" onChange={onChange} ariaLabel="Source" {...over} />)
  return onChange
}

describe('SearchSelect', () => {
  it('shows placeholder and opens the option list', async () => {
    const user = userEvent.setup()
    renderSelect()
    expect(screen.getByText('Pick source')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Source' }))
    expect(screen.getByPlaceholderText('Type to filter…')).toBeInTheDocument()
    expect(screen.getByText('public.companies.id')).toBeInTheDocument()
    expect(screen.getByText('sales.regions.code')).toBeInTheDocument()
  })

  it('filters by label and value, Enter picks the first match', async () => {
    const user = userEvent.setup()
    const onChange = renderSelect()
    await user.click(screen.getByRole('button', { name: 'Source' }))
    await user.type(screen.getByPlaceholderText('Type to filter…'), 'regions')
    expect(screen.queryByText('public.companies.id')).not.toBeInTheDocument()
    expect(screen.getByText('sales.regions.code')).toBeInTheDocument()
    await user.keyboard('{Enter}')
    expect(onChange).toHaveBeenCalledWith('2')
  })

  it('clicking an option selects and closes', async () => {
    const user = userEvent.setup()
    const onChange = renderSelect()
    await user.click(screen.getByRole('button', { name: 'Source' }))
    await user.click(screen.getByText('public.orders.company_id'))
    expect(onChange).toHaveBeenCalledWith('1')
    expect(screen.queryByPlaceholderText('Type to filter…')).not.toBeInTheDocument()
  })

  it('shows empty text when nothing matches', async () => {
    const user = userEvent.setup()
    renderSelect({ emptyText: 'Nothing here' })
    await user.click(screen.getByRole('button', { name: 'Source' }))
    await user.type(screen.getByPlaceholderText('Type to filter…'), 'zzz-nope')
    expect(screen.getByText('Nothing here')).toBeInTheDocument()
  })

  it('displays the selected label on the trigger', () => {
    renderSelect({ value: '1' })
    expect(screen.getByRole('button', { name: 'Source' })).toHaveTextContent('public.orders.company_id')
  })
})
