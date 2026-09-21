import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import type { ConnFields } from '../components/ConnectionBar'
import { apiClient } from '../lib/api'
import { dropSnapshot } from '../lib/schemaCache'
import { forgetSessionConn, readSessionConns, rememberSessionConn } from '../lib/reconnect'
import { onUnauthorized, useAppPreference } from '../lib/storage'
import { readJSON } from '../lib/tabs'
import type { SavedConnection, SessionInfo } from '../types'
import type { VaultStatus } from '../lib/api'

/* Multi-session connection state: N live backend pools. Tabs bind to one
   session id each; the explorer follows the active session; txn controls
   live in each tab's own toolbar, bound to that tab's session. No passwords
   persist. Tab-id remapping on reconnect is injected (tabs owner). */
export function useSessions({ onRemap }: { onRemap: (oldSid: string, newSid: string) => void }) {
  const [fields, setFields] = useState<ConnFields>({ host: 'localhost', port: '5432', user: 'postgres', password: '', dbname: 'postgres', sslmode: 'prefer' })
  const [saved, setSaved] = useState<SavedConnection[]>([])
  const [vault, setVault] = useState<VaultStatus>({ exists: false, unlocked: false })
  const [vaultWarningShown, setVaultWarningShown] = useState(false)
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [activeId, setActiveId] = useState<string>('')
  const active = sessions.find((s) => s.id === activeId) ?? null
  const session = active?.id ?? ''
  const connected = !!active
  useEffect(() => {
    void apiClient.listConnections().then((j) => {
      setSaved((j.connections ?? []).map((c) => ({ id: c.id, name: c.name, host: c.host, port: String(c.port), user: c.user, password: '', dbname: c.dbname, sslmode: c.sslmode ?? 'prefer', has_password: c.has_password, last_used_at: c.last_used_at, folder_id: c.folder_id, environment: c.environment, color: c.color, description: c.description, favorite: c.favorite, default: c.default, tags: c.tags, options: c.options })))
      setVault((v) => ({ ...v, unlocked: !!j.vault_unlocked }))
    }).catch(() => undefined)
    void apiClient.getVault().then((j) => { setVault({ exists: !!j.exists, unlocked: !!j.unlocked }); if (j.unlocked) setVaultWarningShown(false) }).catch(() => undefined)
  }, [])
  const [inTxnMap, setInTxnMap] = useState<Record<string, boolean>>({})
  const markTxn = useCallback((sid: string, v: boolean) => {
    setInTxnMap((m) => (m[sid] === v ? m : { ...m, [sid]: v }))
  }, [])
  const setInTxn = useCallback(
    (v: boolean) => {
      if (activeId) markTxn(activeId, v)
    },
    [activeId, markTxn],
  )
  const [autocommit, setAutocommit] = useAppPreference<boolean>('autocommit', true)
  const [autoLogin, setAutoLogin] = useAppPreference<boolean>('auto-login', true)
  const [credOpen, setCredOpen] = useAppPreference<boolean>('conn-panel-open', true)
  // Dead-session tracking: backend pools die on server restart while tabs
  // persist. deadIds is state (badges); deadRef mirrors it for stable
  // callbacks; retriedRef bounds auto-reconnect to one attempt per session.
  const [deadIds, setDeadIds] = useState<Record<string, boolean>>({})
  const [bootDone, setBootDone] = useState(false)
  const deadRef = useRef<Record<string, boolean>>({})
  const retriedRef = useRef<Record<string, boolean>>({})
  const remapRef = useRef<Map<string, string>>(new Map())
  const sessionsRef = useRef(sessions)
  const activeIdRef = useRef(activeId)
  useEffect(() => {
    sessionsRef.current = sessions
    activeIdRef.current = activeId
  })
  const reconnectOneRef = useRef<(sid: string, opts?: { silent?: boolean }) => Promise<string>>(() => Promise.resolve(''))
  const reconnectAllRef = useRef<() => Promise<void>>(() => Promise.resolve())

  const clearDead = useCallback((sid: string) => {
    if (!sid || !deadRef.current[sid]) return
    delete deadRef.current[sid]
    setDeadIds((prev) => {
      if (!prev[sid]) return prev
      const n = { ...prev }
      delete n[sid]
      return n
    })
  }, [])

  const markDead = useCallback((sid: string, opts?: { silent?: boolean }) => {
    if (!sid || deadRef.current[sid]) return
    const known = sessionsRef.current.some((s) => s.id === sid) || !!readSessionConns()[sid]
    if (!known) return
    deadRef.current[sid] = true
    setDeadIds((prev) => (prev[sid] ? prev : { ...prev, [sid]: true }))
    if (!opts?.silent) {
      toast.error('Session lost (server restart?) — reconnect from Connections', {
        action: { label: 'Reconnect', onClick: () => void reconnectOneRef.current(sid) },
      })
    }
    if (readJSON<boolean>('auto-login', true) && !retriedRef.current[sid]) {
      retriedRef.current[sid] = true
      void reconnectOneRef.current(sid, { silent: true })
    }
  }, [])

  /* ---------- connection (multi-session) ---------- */
  const bootConnect = useCallback(
    async (f: ConnFields, fresh: boolean, dbnameOver?: string, sidOver?: string, activate = true, quiet = false, profileId?: string): Promise<string> => {
      const dbname = dbnameOver || f.dbname
      const j = await apiClient.connect({
        host: f.host,
        port: Number(f.port) || 5432,
        user: f.user,
        password: f.password,
        dbname,
        sslmode: f.sslmode,
        profile_id: profileId || f.profileId,
        session_id: fresh ? '' : sidOver || session || undefined,
      })
      if (j.error || !j.session_id) {
        if (!quiet) toast.error(j.error ?? 'Connect failed')
        return ''
      }
      const sid = j.session_id
      const info: SessionInfo = j.info
        ? { id: sid, host: j.info.host, port: String(j.info.port ?? f.port), user: j.info.user || f.user, dbname: j.info.dbname || dbname, sslmode: j.info.sslmode || f.sslmode, tls_warn: j.info.tls_warn, profile_id: j.info.profile_id, profile_name: j.info.profile_name }
        : { id: sid, host: f.host, port: f.port, user: f.user, dbname, sslmode: f.sslmode, profile_id: profileId || f.profileId }
      setSessions((prev) => (prev.some((s) => s.id === sid) ? prev.map((s) => (s.id === sid ? info : s)) : [...prev, info]))
      if (activate) setActiveId(sid)
      markTxn(sid, !!j.info?.in_txn)
      rememberSessionConn(sid, { ...f, dbname, profileId: profileId || f.profileId })
      clearDead(sid)
      return sid
    },
    [session, setSessions, setActiveId, markTxn, clearDead],
  )

  // Same connection target already has a pool? Reuse it — never spawn a
  // duplicate pool (each pool holds up to 8 backends; duplicates pile up
  // in pg_stat_activity fast).
  const findSession = useCallback(
    (host: string, port: string | number, user: string, dbname: string, sslmode: string) =>
      sessions.find(
        (s) =>
          s.host === host &&
          String(s.port) === String(port) &&
          s.user === user &&
          s.dbname === dbname &&
          (s.sslmode || '') === (sslmode || ''),
      ),
    [sessions],
  )

  const dropSession = useCallback(
    (sid: string) => {
      forgetSessionConn(sid)
      clearDead(sid)
      const next = sessions.filter((s) => s.id !== sid)
      setSessions(next)
      if (sid === activeId) setActiveId(next[0]?.id ?? '')
      setInTxnMap((m) => {
        if (!(sid in m)) return m
        const n = { ...m }
        delete n[sid]
        return n
      })
    },
    [activeId, sessions, setSessions, setActiveId, clearDead],
  )

  const disconnect = useCallback(
    (sid?: string) => {
      const target = sid ?? activeId
      if (target) apiClient.disconnect(target).catch(() => undefined)
      if (target) dropSnapshot(target)
      if (target) dropSession(target)
      if (sessions.length <= 1) setCredOpen(true)
    },
    [activeId, dropSession, sessions.length, setCredOpen],
  )

  const refreshTxn = useCallback(async () => {
    if (!session) return
    try {
      const j = await apiClient.txn(session, 'status')
      markTxn(session, !!j.in_txn)
    } catch {
      /* heartbeat stays stale; next run surfaces it */
    }
  }, [session, markTxn])

  /* ---------- reconnect (dead session → fresh pool, tabs follow) ---------- */
  const reconnectOne = useCallback(
    async (oldSid: string, opts?: { silent?: boolean }): Promise<string> => {
      const f = readSessionConns()[oldSid]
      if (!f || !f.host) {
        if (!opts?.silent) toast.error('No saved credentials for this session — connect manually')
        return ''
      }
      if (opts?.silent && f.profileId && !vault.unlocked) {
        if (!vaultWarningShown) {
          setVaultWarningShown(true)
          toast.info('Vault is locked — automatic profile reconnect is paused until you unlock it')
        }
        return ''
      }
      const nid = await bootConnect({ ...f }, true, undefined, undefined, false, !!opts?.silent, f.profileId)
      if (!nid) return ''
      forgetSessionConn(oldSid)
      dropSnapshot(oldSid)
      onRemap(oldSid, nid)
      setSessions((prev) => prev.filter((s) => s.id !== oldSid))
      setActiveId((a) => (a === oldSid ? nid : a))
      clearDead(oldSid)
      if (!opts?.silent) toast.success(`Reconnected ${f.user}@${f.host}/${f.dbname}`)
      return nid
    },
    [bootConnect, onRemap, clearDead, setSessions, setActiveId, vault.unlocked, vaultWarningShown],
  )

  const reconnectAll = useCallback(async () => {
    const ids = Object.keys(deadRef.current)
    let ok = 0
    for (const id of ids) {
      try {
        const nid = await reconnectOne(id, { silent: true })
        if (nid) ok++
      } catch {
        /* keep going — remaining sessions still get their attempt */
      }
    }
    if (ok) toast.success(`Reconnected ${ok} session${ok > 1 ? 's' : ''}`)
    else toast.error('Reconnect failed — check credentials')
  }, [reconnectOne])

  const connect = useCallback(
    (fresh: boolean, dbnameOver?: string) => {
      if (fresh) {
        const dbname = dbnameOver || fields.dbname
        const hit = findSession(fields.host, fields.port, fields.user, dbname, fields.sslmode)
        if (hit) {
          if (deadIds[hit.id]) {
            return reconnectOne(hit.id).then((nid) => {
              if (nid) setActiveId(nid)
              return nid
            })
          }
          setActiveId(hit.id)
          return Promise.resolve(hit.id)
        }
      }
      return bootConnect(fields, fresh, dbnameOver, undefined, true, false, fields.profileId)
    },
    [bootConnect, fields, findSession, deadIds, reconnectOne, setActiveId],
  )

  const connectProfile = useCallback(
    (profile: SavedConnection, password = '') => {
      const f: ConnFields = {
        host: profile.host,
        port: profile.port,
        user: profile.user,
        password,
        dbname: profile.dbname,
        sslmode: profile.sslmode,
        profileId: profile.id,
      }
      setFields(f)
      const hit = findSession(f.host, f.port, f.user, f.dbname, f.sslmode)
      if (hit) {
        if (deadIds[hit.id]) {
          return reconnectOne(hit.id).then((nid) => {
            if (nid) setActiveId(nid)
            return nid
          })
        }
        setActiveId(hit.id)
        return Promise.resolve(hit.id)
      }
      return bootConnect(f, true, undefined, undefined, true, false, profile.id)
    },
    [bootConnect, deadIds, findSession, reconnectOne, setActiveId],
  )

  const vaultAction = useCallback(async (action: 'setup' | 'unlock' | 'lock' | 'change_password', masterPassword?: string, newPassword?: string) => {
    const j = await apiClient.vaultAction(action, masterPassword, newPassword)
    if (j.error) throw new Error(j.error)
    setVault({ exists: !!j.exists, unlocked: !!j.unlocked })
    if (action === 'setup' || action === 'unlock' || action === 'change_password') setVaultWarningShown(false)
    if (action === 'lock') {
      for (const [sid, f] of Object.entries(readSessionConns())) rememberSessionConn(sid, { ...f, password: '' })
    }
  }, [])
  // Lock the server-side vault when this browser session ends. Beacon is
  // designed to survive pagehide cancellation where fetch often does not.
  useEffect(() => {
    const lockVaultOnExit = () => {
      if (!navigator.sendBeacon) return
      const body = new Blob([JSON.stringify({ action: 'lock' })], { type: 'application/json' })
      navigator.sendBeacon('/api/vault', body)
    }
    window.addEventListener('pagehide', lockVaultOnExit)
    return () => window.removeEventListener('pagehide', lockVaultOnExit)
  }, [])

  const doTxn = useCallback(
    async (action: string, sidOver?: string) => {
      const sid = sidOver ?? session
      if (!sid) return
      try {
        const j = await apiClient.txn(sid, action)
        if (j.error) toast.error(j.error)
        markTxn(sid, !!j.in_txn)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [session, markTxn],
  )

  // Txn controls live inside each tab's own toolbar, bound to that tab's
  // session — never the globally active one.
  const txnFor = useCallback(
    (sid: string) => {
      const s = sessions.find((x) => x.id === sid)
      return {
        inTxn: !!inTxnMap[sid],
        autocommit,
        connLabel: s ? `${s.user}@${s.host}/${s.dbname}` : '',
        connTip: s ? `${s.user}@${s.host}:${s.port}/${s.dbname}` : '',
        onAutocommit: setAutocommit,
        onTxn: (a: string) => void doTxn(a, sid),
      }
    },
    [sessions, inTxnMap, autocommit, setAutocommit, doTxn],
  )

  // Reconcile stored sessions against the live server (a restart invalidates
  // all pools). Adopts the server list when non-empty; stored sessions
  // missing from the server are KEPT (badged dead, reconnectable) — never
  // silently dropped. Returns the live sessions for the boot remap.
  const reconcileSessions = useCallback(async (): Promise<SessionInfo[]> => {
    try {
      const j = await apiClient.listSessions()
      const alive = Array.isArray(j.sessions) ? j.sessions : []
      if (alive.length) {
        const mapped: SessionInfo[] = alive.map((s) => ({
          id: s.id,
          host: s.host || '',
          port: String(s.port ?? ''),
          user: s.user || '',
          dbname: s.dbname || 'postgres',
          sslmode: s.sslmode || '',
          tls_warn: s.tls_warn,
          profile_id: s.profile_id,
          profile_name: s.profile_name,
        }))
        setSessions((prev) => {
          const ids = new Set(mapped.map((s) => s.id))
          return [...mapped, ...prev.filter((s) => !ids.has(s.id))]
        })
        for (const s of alive) if (s.in_txn) markTxn(s.id, true)
        setActiveId((prev) => (mapped.some((s) => s.id === prev) ? prev : (mapped[0]?.id ?? '')))
        return mapped
      }
    } catch {
      /* server down — boot falls through to per-session reconnect */
    }
    return []
  }, [setSessions, setActiveId, markTxn])

  // Backend 401s (dead session) feed markDead: silent auto-retry once,
  // toast with Reconnect action otherwise. Heartbeat catches server
  // restarts while the page stays open (no failed query needed).
  useEffect(() => {
    reconnectOneRef.current = reconnectOne
    reconnectAllRef.current = reconnectAll
  })
  useEffect(() => onUnauthorized((sid) => markDead(sid)), [markDead])
  useEffect(() => {
    const check = async () => {
      try {
        const j = await apiClient.listSessions()
        const alive = new Set((Array.isArray(j.sessions) ? j.sessions : []).map((s) => s.id))
        for (const s of sessionsRef.current) {
          if (!alive.has(s.id)) markDead(s.id)
        }
      } catch {
        /* server down — retry on next tick, no toast spam */
      }
    }
    const timer = setInterval(check, 30000)
    const onVis = () => {
      if (document.visibilityState === 'visible') void check()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [markDead])

  // Boot: reconcile, then reconnect each dead stored session 1:1 (parallel
  // credentials from session-conns) so tabs keep their own DB. Tab restore
  // runs in the App effect below once bootDone flips.
  const booted = useRef(false)
  useEffect(() => {
    if (booted.current) return
    booted.current = true
    void (async () => {
      const alive = await reconcileSessions()
      const aliveIds = new Set(alive.map((s) => s.id))
      const stored = sessionsRef.current
      const auto = readJSON<boolean>('auto-login', true)
      if (auto) {
        for (const s of stored) {
          if (aliveIds.has(s.id) || !readSessionConns()[s.id]) continue
          const nid = await reconnectOneRef.current(s.id, { silent: true })
          if (nid) {
            remapRef.current.set(s.id, nid)
            aliveIds.add(nid)
          }
        }
        // Still-unmapped stored sessions are dead — badge, don't drop.
        for (const s of stored) {
          if (!aliveIds.has(s.id) && !remapRef.current.has(s.id)) markDead(s.id, { silent: true })
        }
        const lost = stored.filter((s) => !aliveIds.has(s.id))
        if (lost.length && aliveIds.size) {
          toast.error(`${lost.length} session${lost.length > 1 ? 's' : ''} lost (server restart?) — tabs kept`, {
            action: { label: 'Reconnect all', onClick: () => void reconnectAllRef.current() },
          })
        }
      }
      try {
        localStorage.removeItem('sid')
      } catch {
        /* legacy key — ignore */
      }
      setBootDone(true)
    })()
    // Boot-only effect by design (guarded by ref, not deps).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return {
    fields, setFields, saved, setSaved, sessions, setSessions, connectProfile, vault, vaultAction,
    active, activeId, setActiveId, session, connected,
    inTxnMap, markTxn, setInTxn, autocommit, setAutocommit,
    autoLogin, setAutoLogin, credOpen, setCredOpen,
    deadIds, deadRef, remapRef, sessionsRef, activeIdRef, bootDone,
    markDead, clearDead, bootConnect, findSession, dropSession, disconnect,
    refreshTxn, reconnectOne, reconnectAll, connect, doTxn, txnFor,
    reconcileSessions,
  }
}

export type UseSessions = ReturnType<typeof useSessions>
