import { afterEach, describe, expect, it, vi } from 'vitest'
import { saveResponseStream } from '@/lib/format'

afterEach(() => vi.unstubAllGlobals())

describe('streamed CSV download', () => {
  it('writes response chunks incrementally to a file handle', async () => {
    const writes: number[][] = []
    const close = vi.fn(async () => undefined)
    const picker = vi.fn(async () => ({
      createWritable: async () => ({
        write: async (chunk: Uint8Array) => { writes.push([...chunk]) },
        close,
      }),
    }))
    vi.stubGlobal('window', { showSaveFilePicker: picker })
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('id,value\n'))
        controller.enqueue(new TextEncoder().encode('1,alpha\n'))
        controller.close()
      },
    })

    await saveResponseStream(new Response(stream), 'rows.csv')

    expect(picker).toHaveBeenCalledWith({ suggestedName: 'rows.csv' })
    expect(writes).toEqual([[105, 100, 44, 118, 97, 108, 117, 101, 10], [49, 44, 97, 108, 112, 104, 97, 10]])
    expect(close).toHaveBeenCalledOnce()
  })
})
