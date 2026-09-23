# pglight — Project Rules (frontend + backend)

> Rules for every contributor (human or agent). Follow them; if a rule blocks
> you, say so instead of silently breaking it.

## 0. Stack map

- **Backend**: Go 1.26.8, `github.com/jackc/pgx/v5`. API in `internal/api/`
  (domain files: `session|explorer|query|data|admin|alter|complete|settings|aliases`
  + `connections|preferences|appdata|mockdata|vault`, shared kernel in
  `handlers.go`, autocomplete cache in `complete_cache.go`), connection pool +
  explicit-txn state + operation leases in `internal/db/manager.go`, app data
  (sqlite `data/pglight.db`: users, snippets, history, aliases, connection
  profiles/folders/tags/options, vaults/secrets, preferences, ERD layouts) in
  `internal/store/`, pure mock-data engine in `internal/mockgen/` (HTTP
  orchestration in `mockdata.go`), HTTP/query/txn logging in `internal/logging/`,
  flat route table in `main.go` (53 `/api/*` registrations; `Manager.CloseAll`
  + `os.Exit` shutdown).
- **Frontend**: React 18 + Vite 5 + Tailwind v3 + shadcn-style prebuilt
  components (`web/src/components/ui/*`: Button, Input, Textarea, PasswordTextarea (masked multiline secrets such as SSH private keys), Badge,
  Card, Table, Tabs, Dialog, AlertDialog, Select, SearchSelect, Separator,
  ScrollArea, Collapsible, Switch, Resizable, ContextMenu, DropdownMenu,
  Popover, Tooltip/`Tip`, DataGrid) + CodeMirror SQL editor (`SqlEditor`,
  `lib/complete.ts` + `lib/schemaCache.ts`) + ERD canvas (`@xyflow/react`,
  `components/erd/`) + Sonner toasts + promise-based dialog host
  (`dialogs.tsx`: `confirm`/`prompt`/`promptNullable`/`form`).
  Domain logic in `hooks/` (`useSessions|useTabs|useSplit|useExplorer|`
  `useQueryRunner|useTableOps|useObjectOps|useGridSelection`); tab kinds
  `query|table|browser|erd|docs|object` (`types.ts`, helpers in `lib/tabs.ts`).
  Build output `web/dist` is **git-ignored** (local build artifact) — `main.go` embeds it.
- **Test DB**: `docker/` → postgres:14-alpine + `init.sql` seed.

## 1. Golden commands

```sh
./scripts/rerun.sh                  # kill :8080, rebuild, rerun (PORT=… to change)
docker compose -f docker/docker-compose.yml up -d   # test DB
cd web && npm run dev              # Vite :5173, proxies /api → :8080
```

Gate before finishing any change: `./scripts/check.sh` (gofmt, vet, test, build,
  tsc, eslint; on Windows `scripts/check.bat`). CI (`.github/workflows/build.yml`,
  Node 22, postgres:14 service) additionally runs `go test -race`,
  `govulncheck`, and `npm test` (vitest/jsdom) plus the 9-target release
  matrix — run those locally when you touch txn concurrency, deps, or
  frontend logic. Frontend tests use Node 22+ (`jsdom@30` requires it; Node 18
  fails with `ERR_REQUIRE_ESM`). Frontend
change ⇒ `cd web && npm run build` so the local `dist/` stays fresh (`dist/`
is git-ignored: rebuild it, never commit it).
- **Frontend test layout:** all Vitest tests (`*.test.*`/`*.spec.*`) live in
  `web/src/test/`. Keep shared test setup/helpers there as well; do not place
  test files beside production components, hooks, libraries, or shortcuts.

## 2. Backend rules (Go)

1. **Session**: every endpoint takes `session_id` (query param, `X-Session-Id`
   header, or JSON body). Use `sessionID(r)` / `sessionFromBody()`.
2. **Txn-awareness**: any endpoint that reads/writes user data MUST go through
   `Mgr.Q(id)` (`db.Querier`), never `Mgr.Get()` directly — otherwise explicit
   transactions (`POST /api/txn`) are silently bypassed. Helpers `queryJSON`
   and `execQuery` already take a `Querier`; reuse them.
3. **Safety guards** (pgAdmin-style, never remove):
   - Refuse `UPDATE`/`DELETE` without `WHERE` in `/api/row`.
   - `/api/maintenance` allow-lists `vacuum|vacuum_full|analyze|reindex` only,
     and refuses inside an open txn (VACUUM can't run in a txn block).
   - `/api/import` caps JSON imports at 20k rows and batches of 500; streaming
     `/api/import/csv` accepts multipart uploads up to 2 GiB and batches 500
     records. Both paths are atomic (own txn unless the session already has one).
4. **SQL hygiene**: identifiers via `pgx.Identifier{}.Sanitize()`; values via
   `$n` params. No `fmt.Sprintf` interpolation of user input into SQL —
   `filter`/`order` in `/api/table-data` are the only exception and must keep
   the existing identifier-sanitize + direction allow-list.
5. **Response contract**: success JSON keeps existing shapes (additive changes
   only); errors are always `{error: string}`; query-like responses include
   `in_txn: bool`. Multi-statement scripts return `{results[]}`.
6. **Limits**: cap result sets (1000 rows), add `LIMIT` to explorer queries,
   use 5–30s contexts (120s only for maintenance/import).
7. **New endpoint checklist**: handler in its domain file (rule 10) → route in `main.go` →
   typed client in `web/src/lib/api.ts` → document in `PLAN.md` → curl-verify
   against the docker DB.
8. **Observability (AOP)**: no ad-hoc logging in handlers. HTTP is logged by
   `logging.Middleware` in `main.go`; queries by the `logging.Querier`
   wrapper applied once in `Handler.q`; txn by `LogTxn`. Steady-state
   queries are `debug`, slow ones `warn`, failures `error`. Runtime config
   persists in `data/logging.json` and is edited only via `/api/settings` +
   the web Settings panel — never by hand-editing the file.
9. Formatting: `gofmt` clean, `go vet` clean, no new deps without a reason.
10. **Domain layout (DDD-lite)**: one bounded context = one file `internal/api/<domain>.go`
    owning its handler methods + request/response types + process-wide store.
    Current domains: `session` (connect/sessions/disconnect/txn),
    `explorer` (databases/schemas/tables/objects/columns/ddl/defs/constraints/
    triggers/stats/types/erd), `query` (query/explain), `data` (table-data/
    search/import/row/rows-delete), `admin` (activity/locks/server-info/stats/
    roles/extensions/maintenance/cancel/shutdown), `alter`, `complete`
    (+ `complete_cache.go`), `settings` (settings/logs), `aliases`,
    `connections`, `preferences`, `appdata` (snippets/history), `mockdata`
    (HTTP orchestration over the pure `internal/mockgen/` engine), `vault`
    (credential-vault state and actions).
    `handlers.go` owns only the shared kernel
    (`Handler`, `writeJSON`, `sessionID`/`sessionFromBody`, `q`/`pool`, `queryJSON`/`execQuery`,
    `rowsToMaps`, `errLocation`, cross-domain detectors like `ddlRe`). NEVER copy kernel
    helpers into a domain file; NEVER touch another domain's unexported state — use its
    exported API (`globalComplete.Invalidate`, `globalAliases.List`). No `misc.go`/`utils.go` catch-alls.
    App data lives in `internal/store/` (sqlite tables `app_users`, `snippets`,
    `query_history`, `aliases`, `connection_profiles`, `connection_folders`,
    `connection_tags`, `connection_options`, `vaults`, `connection_secrets`,
    `connection_ssh_secrets`,
    `user_preferences`, `erd_layouts`) — never in PG target connections.

## 3. Frontend rules (React + shadcn)

1. **Prebuilt UI only**: compose from `components/ui/*` (Button, Input,
   Textarea, PasswordTextarea, Badge, Card, Table, Tabs, Dialog, AlertDialog, Select,
   SearchSelect, Separator, ScrollArea, Collapsible, Switch, Resizable,
   ContextMenu, DropdownMenu, Popover, Tooltip/`Tip`, DataGrid) +
   `feedback.tsx` (Skeleton/ErrorText/
   EmptyNote). Do NOT hand-roll buttons/inputs/modals/tables, do NOT add raw
   CSS files — style via `cn()` + Tailwind theme tokens. New generic widget?
   Add it to `ui/` in shadcn style (cva variants, Radix under the hood).
   Known drift (do not extend): a few feature components still contain raw
   buttons/inputs — migrate them to `ui/*` when touched.
   **Prebuilt priority:** MUST use an existing prebuilt component whenever one
   is suitable before implementing new markup or interaction primitives. If no
   suitable prebuilt component exists, create a reusable shadcn-style component
   in `web/src/components/ui/` (or the appropriate shared components directory)
   and update this `AGENTS.md` with the new component and its intended reuse.
2. **No native dialogs, ever**: no `alert/confirm/prompt` —
   - notifications → `toast.success/error/info` (Sonner; monochrome thin style
     on `<Toaster>` in `App.tsx`, don't restyle per-call),
   - confirmations → `dialogs.confirm({title, description, confirmText, danger})`,
   - single input → `dialogs.prompt({title, description, defaultValue})`
     (nullable variant `promptNullable` returns `{isNull:true}` for explicit
     Set-NULL; `null` still means cancelled),
   - multi-field input → `dialogs.form({title, fields[]})` (one dialog, not a
     prompt loop; resolves `Record<string, string | null> | null`).
   `dialogs` comes from `createDialogs` in `App.tsx`; pass it
   down as a prop (`dialogs: DialogsApi`).
3. **Icons**: `lucide-react` only, no emoji in UI chrome.
4. **Data access**: all HTTP via `lib/api.ts` (`api`/`apiClient`/`q(session,…)`).
   No `fetch` elsewhere. LocalStorage only via `useLocalStorage` (or the
   `useLocalStorage` hook in `lib/storage.ts`). Exceptions: ERD node layout +
   viewport persist raw via `components/erd/erdStorage.ts` (keys
   `pglight-erd-layout` / `pglight-erd-viewport`, session+schema scoped), and
   the CodeMirror completion path in `lib/schemaCache.ts` (direct `fetch` to
   `/api/complete` with in-memory snapshot + single-flight — do not copy this
   pattern elsewhere).
5. **State**: tabs are immutable (`setTabs(prev => prev.map(…))`, narrow by
   `t.kind`: `query|table|browser|erd|docs|object`); per-tab loaders are
   `loadTablePage`/`loadTableMeta`-style
   functions in `App.tsx`. Feature components stay presentational: props in,
   callbacks out — no cross-component imports. Domain logic lives in `hooks/`
   (`useSessions`, `useTabs`, `useSplit`, `useExplorer`, `useQueryRunner`,
   `useTableOps`, `useObjectOps`); shared selection in `useGridSelection`;
   pure helpers (`qi`, tab-id builders, `slimTab`/restore, `pkOf`, `DDL_RE`)
   in `lib/tabs.ts`.
6. **Grids**: result/object tables use `<DataGrid>` or `ui/table` parts; cell
   values truncate (`max-w-[320px]`), NULL renders italic.
7. **Tooltips — one global style**: always `<Tip>` (`components/ui/tooltip.tsx`,
   dark `bg-popover` card). NEVER native `title=` (OS white box) on any element —
   dense cells/rows (DataGrid, TableWorkspace, ERD nodes) wrap truncated content
   with `<Tip>` too; CodeMirror autocomplete inherits `--popover` tokens.
8. **TS strict**: no `any`, no unused imports/locals, `@/` path alias (kept in
   sync in `tsconfig.json` + `vite.config.ts`). ESLint must pass;
   `eslint-disable` needs a one-line reason comment, never a blanket disable.
9. **New view checklist**: component in `components/` (top-level views:
   `Explorer`, `QueryConsole`/`SqlEditor`, `TableWorkspace` (+`ColumnEditor`,
   `IndexTriggerEditor`, `MockDataDialog`), `ObjectView`, `BrowserView`,
   `ErdView` (+`components/erd/`), `SidePanel` (history/snippets/aliases/
   server/activity/locks/stats/settings/logs), `SearchPalette`,
   `CredentialManager`, `ConnectionBar`, `SettingsPanel`, `LogsPanel`,
   `AliasesPanel`, `DocsView`, `TabStrip`/`TabContent`/`SplitWorkspace`,
   `TxnControls`) → wire into `App.tsx`
   tabs/side/explorer → reuse `ui/*` + `dialogs`/`toast` → `npm run build`.

## 4. Test DB rules

- `docker/init.sql` must stay re-runnable from scratch (`down -v` reseeds).
  It must cover every explorer group: tables with FK (`authors` → `books` →
  `reviews`), view (`published_books`), matview (`author_stats`), function
  (`book_count_by_status`), sequence defaults, enum type (`mood`), trigger
  (`trg_books_touch`), indexes, check/unique constraints, plus the
  mock-generator fixture (`mock_users`: BIGSERIAL pk, UNIQUE email,
  `BETWEEN` + `IN` CHECKs, `created_at <= updated_at`).
- `docker/sample-authors.csv` stays in sync with the `authors` table for
  import testing. Never commit real credentials — the `postgres/postgres`
  test-only login lives in the compose file, nowhere else.

## 5. Completed functionality (v0.2, `web` 0.2.0)

The following non-auth functionality is implemented and covered by the current
backend/frontend surface:

- PostgreSQL sessions: saved connection profiles (sqlite `connection_profiles`
  + legacy localStorage migration on boot), multiple live sessions,
  reconnect-after-restart, per-session transactions (serial querier +
  `AcquireLease` operation lease + 15min abandoned-txn sweeper), and
  transaction-aware query/data paths. Safe DSN building (spaces/IPv6),
  sslmode allow-list (`disable|prefer|require|verify-ca|verify-full`), TLS
  warnings for unverified non-loopback links.
- Credential vault and recovery: versioned Argon2id + AES-GCM vault records,
  atomic master-password rotation, 15-minute inactivity auto-lock,
  session-end locking, unlock backoff,
  encrypted profile export/import, and clear-on-lock runtime keys. The only
  reset flow is offline `pglight.exe vault reset`: it requires an existing
  store, an exclusive OS lock, confirmation (or `--password-stdin --yes`),
  and a new master password; it removes stored PostgreSQL and SSH credentials while
  preserving profiles, folders, tags, and non-secret metadata. It does not
  erase history/snippets/backups or change PostgreSQL passwords.
- SSH tunnel transport for saved profiles uses a loopback-only forward, vault-encrypted SSH credentials, SHA256 host-key pinning, and session-scoped teardown. Configuration belongs in Workspace → Connections → Advanced; PostgreSQL TLS options remain separate.
- Explorer and schema work: databases, schemas, tables, views, materialized
  views, foreign tables, functions, sequences, types, indexes, triggers,
  constraints, table statistics, DDL, ERD (`@xyflow/react` canvas:
  column-level FK edges, auto-layout, drag/viewport persistence, search,
  minimap), global search, and context-aware cached autocomplete
  (`/api/complete` + ETag/304 + DDL invalidation).
- Query and data tools: single- and multi-statement queries (per-result
  limits 200/1000/5000/10000/no-limit with 64 MiB guard → 413), EXPLAIN,
  history, snippets, cancellation (`application_name=pglight:<session>` +
  Dashboard), paging/filtering/ordering (`has_more`), guarded row edits
  (PK-scoped, `single:true` 409 on non-1-row, real JSON null — no
  `__NULL__` sentinel), bulk delete, CSV import, CSV/JSON/INSERT export
  (OID-aware exact numerics: int8/numeric/money as strings + `UseNumber`
  decode), maintenance, and mock-data generation (Simple zero-config vs
  Advanced schema-aware: semantic generators, ranges, FK pools incl.
  composite, CHECK inference, relative datetimes, `meta/preview≤100/`
  `generate≤20000` — pure engine `internal/mockgen/`, txn-aware HTTP
  `mockdata.go`).
- React workspace: shadcn-style UI components, table/query grids, 2-pane
  split view (`useSplit`, pinned tab, direction/swap), dashboard panels
  (Server/Activity/Locks/Stats), object editors (function/sequence/type),
  `DocsView`, session-aware tab restore (capped snapshots, privacy-gated),
  Sonner/promise-based dialogs, and `POST /api/shutdown` Power button.
- Observability and local app data: HTTP/query/transaction logging with the
  Settings panel (`data/logging.json` via `/api/settings` only), aliases
  (builtin + user overrides), snippets/history/connections/preferences
  (sqlite `Store`, `PGLIGHT_STORE`/`PGLIGHT_USER_ID` overridable),
  privacy controls (history persistence, snapshot restore default off,
  retention prune, clear-all), and persisted ERD layouts.
- Application controls and recovery: bilingual in-app `DocsView`, command
  palette plus rebindable/validated shortcuts, configurable Workspace Quick
  Access buttons, live LAN-access security toggle, startup port selection and
  browser suppression (`--p/--port`, `PORT`, `--no-browser`), and the offline
  `vault reset` flow with process locking and password-stdin automation.

## 6. Current function inventory (full project scan — 2026-09-21)

The route table currently contains 54 registrations. Keep this inventory in
sync when adding or removing a user-visible function:

- **Sessions and connections**: `connect`, `sessions`, `disconnect`, `txn`,
  `connections`, `connections/folders`, `connections/export`,
  `connections/import`, `connections/test`, and `vault`.
- **Explorer and schema**: `databases`, `schemas`, `tables`, `objects`,
  `columns`, `ddl`, `view-def`, `func-def`, `seq-def`, `type-def`, `types`,
  `constraints`, `triggers`, `table-stats`, `erd`, and `search` (including
  SQLite-backed ERD layout/viewport persistence through `erd?layout=1`).
- **Query and data**: `query` (single and multi-statement), `explain`,
  `complete`, `table-data`, `row`, `rows-delete`, `import` (JSON + streaming
  multipart CSV), `maintenance`,
  `activity`, and `cancel`.
- **App data and administration**: `aliases`, `snippets`, `history`,
  `preferences`, `preferences/shortcuts`, `server-info`, `stats`, `locks`,
  `roles`, `extensions`, `settings`, `logs`, and `shutdown`.
- **Mock data**: `mock-data/meta`, `mock-data/preview`, and
  `mock-data/generate`; generation logic stays pure in `internal/mockgen/` and
  the HTTP layer remains txn-aware.
- **Frontend composition**: tab kinds are `query|table|browser|erd|docs|object`
  plus `workspace`; Workspace views are `history|snippets|aliases|server|
  activity|locks|stats|connections|settings|shortcuts|logs|quick-access`.
  Domain hooks are `useSessions`, `useTabs`, `useSplit`, `useExplorer`,
  `useQueryRunner`, `useTableOps`, `useObjectOps`, and `useGridSelection`.
  Shared UI functions include SQL formatting/export, schema-cache completion,
  ERD layout/storage, dialog promises, command registration, and shortcut
  normalization.

When changing one of these functions, update `PLAN.md` and the bilingual
`DocsView` when it affects user behavior, then run the frontend build so the
embedded `web/dist` stays current locally (without committing it).

## 7. Docs & commits

- New endpoint/component/behavior ⇒ update `PLAN.md` (API list / stack section).
- Never commit `web/dist` (git-ignored build output), `web/node_modules`, `*.tsbuildinfo`, `data/`, binaries, or `.env` files.
- **One feature = one commit.** Finish order: implement → `./scripts/check.sh`
  → rebuild `dist` → smoke-test (API + UI) → commit immediately. Message in
  conventional style: `feat|fix|docs|chore|refactor(scope): subject`, e.g.
  `feat(observability): AOP request/query logging with web settings`.
