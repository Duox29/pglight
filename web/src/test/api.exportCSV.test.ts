import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/lib/api'

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('native streamed CSV download', () => {
  it('submits a hidden same-origin form and reports API errors', () => {
    const submitted: HTMLFormElement[] = []
    vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function (this: HTMLFormElement) {
      submitted.push(this)
    })
    const onError = vi.fn()

    apiClient.exportCSV({
      session_id: 'session-test',
      schema: 'public',
      table: 'authors',
      filter: 'id > 1',
      order: 'id DESC',
    }, onError)

    const form = submitted[0]
    expect(form).toBeDefined()
    if (!form) throw new Error('CSV form was not submitted')
    expect(form.method).toBe('post')
    expect(new URL(form.action).pathname).toBe('/api/export/csv')
    expect(form.enctype).toBe('application/x-www-form-urlencoded')
    expect(form.target).toBe('pglight-csv-download')
    expect([...form.elements].map((element) => (element as HTMLInputElement).value)).toEqual([
      'session-test', 'public', 'authors', 'id > 1', 'id DESC',
    ])

    const frame = document.querySelector<HTMLIFrameElement>('iframe[name="pglight-csv-download"]')
    expect(frame).not.toBeNull()
    frame!.contentDocument!.body.textContent = '{"error":"not connected"}'
    frame!.dispatchEvent(new Event('load'))
    expect(onError).toHaveBeenCalledWith('not connected')
  })
})
