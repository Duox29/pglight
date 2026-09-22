import { useEffect } from 'react'
import { scrollbarConfig } from '@/lib/scrollbar'

const LAYER_ATTRIBUTE = 'data-pglight-scrollbar-layer'
const VIEWPORT_SELECTOR = '[data-radix-scroll-area-viewport]'
const MIN_THUMB_SIZE = 18

type Axis = 'vertical' | 'horizontal'

type FloatingBar = {
  element: HTMLDivElement
  axis: Axis
}

type ScrollEntry = {
  element: HTMLElement
  vertical: FloatingBar
  horizontal: FloatingBar
  onScroll: () => void
  onEnter: () => void
  onLeave: () => void
}

function isScrollable(element: HTMLElement, axis: Axis) {
  const style = getComputedStyle(element)
  const overflow = axis === 'vertical' ? style.overflowY : style.overflowX
  if (!['auto', 'overlay', 'scroll'].includes(overflow)) return false
  return axis === 'vertical'
    ? element.scrollHeight > element.clientHeight + 1
    : element.scrollWidth > element.clientWidth + 1
}

function createBar(layer: HTMLDivElement, axis: Axis): FloatingBar {
  const element = document.createElement('div')
  element.dataset.pglightScrollbar = axis
  element.style.position = 'fixed'
  element.style.zIndex = '2147483647'
  element.style.pointerEvents = 'none'
  element.style.borderRadius = '9999px'
  element.style.background = 'hsl(var(--border))'
  element.style.opacity = '0'
  element.style.transition = 'opacity 140ms ease'
  element.style.willChange = 'transform, width, height'
  layer.appendChild(element)
  return { element, axis }
}

function setBarGeometry(bar: FloatingBar, element: HTMLElement) {
  const rect = element.getBoundingClientRect()
  const hasOverflow = bar.axis === 'vertical'
    ? element.scrollHeight > element.clientHeight + 1
    : element.scrollWidth > element.clientWidth + 1
  if (!hasOverflow || rect.width <= 0 || rect.height <= 0) {
    bar.element.style.display = 'none'
    return false
  }
  bar.element.style.display = 'block'

  if (bar.axis === 'vertical') {
    const trackSize = rect.height
    const thumbSize = Math.max(MIN_THUMB_SIZE, trackSize * element.clientHeight / element.scrollHeight)
    const maxScroll = element.scrollHeight - element.clientHeight
    const offset = maxScroll > 0 ? element.scrollTop / maxScroll * (trackSize - thumbSize) : 0
    bar.element.style.left = `${Math.max(0, rect.right - 7)}px`
    bar.element.style.top = `${rect.top + offset}px`
    bar.element.style.width = '6px'
    bar.element.style.height = `${Math.min(trackSize, thumbSize)}px`
  } else {
    const trackSize = rect.width
    const thumbSize = Math.max(MIN_THUMB_SIZE, trackSize * element.clientWidth / element.scrollWidth)
    const maxScroll = element.scrollWidth - element.clientWidth
    const offset = maxScroll > 0 ? element.scrollLeft / maxScroll * (trackSize - thumbSize) : 0
    bar.element.style.left = `${rect.left + offset}px`
    bar.element.style.top = `${Math.max(0, rect.bottom - 7)}px`
    bar.element.style.width = `${Math.min(trackSize, thumbSize)}px`
    bar.element.style.height = '6px'
  }
  return true
}

function setVisible(entry: ScrollEntry, visible: boolean) {
  const setBarOpacity = (bar: FloatingBar) => {
    bar.element.style.opacity = visible && bar.element.style.display !== 'none' ? '0.9' : '0'
  }
  setBarOpacity(entry.vertical)
  setBarOpacity(entry.horizontal)
}

function updateEntry(entry: ScrollEntry) {
  setBarGeometry(entry.vertical, entry.element)
  setBarGeometry(entry.horizontal, entry.element)
  setVisible(entry, scrollbarConfig.visible === 'always'
    || (scrollbarConfig.visible === 'hover' && entry.element.matches(':hover')))
}

function isIgnored(element: HTMLElement) {
  return Boolean(element.closest(`[${LAYER_ATTRIBUTE}]`) || element.matches(VIEWPORT_SELECTOR))
}

function getScrollableElements() {
  return Array.from(document.body.querySelectorAll('*')).filter(
    (node): node is HTMLElement => node instanceof HTMLElement && !isIgnored(node)
      && (isScrollable(node, 'vertical') || isScrollable(node, 'horizontal')),
  )
}

export function FloatingScrollbars() {
  useEffect(() => {
    const layer = document.createElement('div')
    layer.dataset.pglightScrollbarLayer = ''
    layer.style.position = 'fixed'
    layer.style.inset = '0'
    layer.style.pointerEvents = 'none'
    layer.style.zIndex = '2147483647'
    document.body.appendChild(layer)

    const entries = new Map<HTMLElement, ScrollEntry>()
    const pending = new Set<ScrollEntry>()
    let frame = 0
    let fullUpdate = true

    const updateAll = () => {
      frame = 0
      if (fullUpdate) {
        fullUpdate = false
        pending.clear()
        for (const entry of entries.values()) updateEntry(entry)
        return
      }
      for (const entry of pending) updateEntry(entry)
      pending.clear()
    }

    const scheduleUpdate = () => {
      if (frame) return
      frame = window.requestAnimationFrame(updateAll)
    }

    const scheduleEntryUpdate = (entry: ScrollEntry) => {
      pending.add(entry)
      scheduleUpdate()
    }

    const resizeObserver = typeof ResizeObserver === 'undefined'
      ? null
      : new ResizeObserver(() => sync())

    const add = (element: HTMLElement) => {
      const entry: ScrollEntry = {
        element,
        vertical: createBar(layer, 'vertical'),
        horizontal: createBar(layer, 'horizontal'),
        onScroll: () => {
          scheduleEntryUpdate(entry)
          if (scrollbarConfig.visible === 'hover') setVisible(entry, element.matches(':hover'))
        },
        onEnter: () => {
          if (scrollbarConfig.visible === 'hover') setVisible(entry, true)
        },
        onLeave: () => {
          if (scrollbarConfig.visible === 'hover') setVisible(entry, false)
        },
      }
      entries.set(element, entry)
      element.addEventListener('scroll', entry.onScroll, { passive: true })
      element.addEventListener('pointerenter', entry.onEnter)
      element.addEventListener('pointerleave', entry.onLeave)
      resizeObserver?.observe(element)
      updateEntry(entry)
      setVisible(entry, scrollbarConfig.visible === 'always'
        || (scrollbarConfig.visible === 'hover' && element.matches(':hover')))
    }

    const remove = (entry: ScrollEntry) => {
      entry.element.removeEventListener('scroll', entry.onScroll)
      entry.element.removeEventListener('pointerenter', entry.onEnter)
      entry.element.removeEventListener('pointerleave', entry.onLeave)
      resizeObserver?.unobserve(entry.element)
      entry.vertical.element.remove()
      entry.horizontal.element.remove()
    }

    const sync = () => {
      const elements = new Set(getScrollableElements())
      for (const [element, entry] of entries) {
        if (!elements.has(element) || !element.isConnected) {
          remove(entry)
          entries.delete(element)
        }
      }
      for (const element of elements) {
        if (!entries.has(element)) add(element)
      }
      fullUpdate = true
      scheduleUpdate()
    }

    const mutationObserver = new MutationObserver((records) => {
      const relevant = records.some((record) => {
        const target = record.target instanceof Element ? record.target : record.target.parentElement
        return !target || !target.closest(`[${LAYER_ATTRIBUTE}]`)
      })
      if (relevant) {
        // DOM churn can happen several times during a React commit. Let the
        // browser settle before doing the one required document scan.
        fullUpdate = true
        scheduleUpdate()
        window.queueMicrotask(sync)
      }
    })

    sync()
    window.addEventListener('resize', sync)
    mutationObserver.observe(document.body, { childList: true, subtree: true })

    return () => {
      window.removeEventListener('resize', sync)
      mutationObserver.disconnect()
      resizeObserver?.disconnect()
      if (frame) window.cancelAnimationFrame(frame)
      for (const entry of entries.values()) remove(entry)
      layer.remove()
    }
  }, [])

  return null
}
