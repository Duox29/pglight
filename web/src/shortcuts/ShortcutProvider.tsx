import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { apiClient } from '@/lib/api'
import { COMMANDS, getCommand } from '@/commands/registry'
import type { CommandHandler, CommandId, CommandRegistrationOptions } from '@/commands/types'
import { DEFAULT_BINDINGS } from './defaults'
import { canDispatch, matchesShortcut } from './matcher'
import { normalizeShortcut, shortcutComparisonKey } from './normalize'
import type { BindingMap } from './types'
import { migrateShortcutSettings } from './migrate'

interface Registration { handler: CommandHandler; options: CommandRegistrationOptions }

interface ShortcutContextValue {
  bindings: BindingMap
  paletteOpen: boolean
  setPaletteOpen: (open: boolean) => void
  register: (id: CommandId, handler: CommandHandler, options?: CommandRegistrationOptions) => () => void
  execute: (id: CommandId) => Promise<boolean>
  setBindings: (id: CommandId, values: string[]) => void
  resetBinding: (id: CommandId) => void
  resetAll: () => void
  conflictFor: (binding: string, except?: CommandId) => CommandId | null
  formatBinding: (id: CommandId) => string[]
}

const Context = createContext<ShortcutContextValue | null>(null)

export function CommandProvider({ children }: { children: ReactNode }) {
  const [overrides, setOverrides] = useState<Partial<Record<CommandId, string[]>>>({})
  const [paletteOpen, setPaletteOpen] = useState(false)
  const registrations = useRef(new Map<CommandId, Registration[]>())
  const latest = useRef<ShortcutContextValue | null>(null)
  const loaded = useRef(false)

  const bindings = useMemo(() => {
    const merged = { ...DEFAULT_BINDINGS, ...overrides }
    return Object.fromEntries(COMMANDS.map((command) => [command.id, merged[command.id] ?? []])) as BindingMap
  }, [overrides])

  const persist = useCallback((next: Partial<Record<CommandId, string[]>>) => {
    void apiClient.saveShortcutSettings(next).catch(() => undefined)
  }, [])

  useEffect(() => {
    let alive = true
    void apiClient.getShortcutSettings().then((response) => {
      if (!alive || loaded.current) return
      loaded.current = true
      setOverrides(migrateShortcutSettings(response).overrides)
    }).catch(() => { loaded.current = true })
    return () => { alive = false }
  }, [])

  const register = useCallback((id: CommandId, handler: CommandHandler, options: CommandRegistrationOptions = {}) => {
    const list = registrations.current.get(id) ?? []
    const registration = { handler, options }
    list.push(registration)
    registrations.current.set(id, list)
    return () => {
      const current = registrations.current.get(id) ?? []
      registrations.current.set(id, current.filter((item) => item !== registration))
    }
  }, [])

  const execute = useCallback(async (id: CommandId) => {
    const list = registrations.current.get(id) ?? []
    const available = list.filter((item) => item.options.enabled === undefined || (typeof item.options.enabled === 'function' ? item.options.enabled() : item.options.enabled))
    const registration = [...available].sort((a, b) => (b.options.priority ?? 0) - (a.options.priority ?? 0))[0]
    if (!registration) return false
    await registration.handler()
    return true
  }, [])

  const setBindings = useCallback((id: CommandId, values: string[]) => {
    const normalized = values.map(normalizeShortcut).filter((value): value is string => value != null).slice(0, 4)
    setOverrides((previous) => {
      const next = { ...previous, [id]: normalized }
      persist(next)
      return next
    })
  }, [persist])

  const resetBinding = useCallback((id: CommandId) => {
    setOverrides((previous) => {
      const next = { ...previous }
      delete next[id]
      persist(next)
      return next
    })
  }, [persist])

  const resetAll = useCallback(() => {
    setOverrides({})
    persist({})
  }, [persist])

  const conflictFor = useCallback((binding: string, except?: CommandId) => {
    const normalized = shortcutComparisonKey(binding)
    if (!normalized) return null
    const entry = Object.entries(bindings).find(([id, values]) => id !== except && values.some((value) => shortcutComparisonKey(value) === normalized))
    return entry?.[0] as CommandId | undefined ?? null
  }, [bindings])

  const formatBinding = useCallback((id: CommandId) => bindings[id] ?? [], [bindings])

  const value = useMemo<ShortcutContextValue>(() => ({ bindings, paletteOpen, setPaletteOpen, register, execute, setBindings, resetBinding, resetAll, conflictFor, formatBinding }), [bindings, paletteOpen, register, execute, setBindings, resetBinding, resetAll, conflictFor, formatBinding])
  useEffect(() => {
    latest.current = value
  }, [value])

  useEffect(() => {
    const handle = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return
      const current = latest.current
      if (!current) return
      for (const command of COMMANDS) {
        if (!canDispatch(command, event)) continue
        if (!current.bindings[command.id].some((binding) => matchesShortcut(event, binding))) continue
        event.preventDefault()
        void current.execute(command.id)
        return
      }
    }
    document.addEventListener('keydown', handle)
    return () => document.removeEventListener('keydown', handle)
  }, [])

  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function useCommand(): ShortcutContextValue {
  const context = useContext(Context)
  if (!context) throw new Error('useCommand must be used inside CommandProvider')
  return context
}

export function useCommandRegistration(id: CommandId, handler: CommandHandler, options?: CommandRegistrationOptions) {
  const { register } = useCommand()
  useEffect(() => register(id, handler, options), [handler, id, options, register])
}

export function commandLabel(id: CommandId): string {
  return getCommand(id).title
}
