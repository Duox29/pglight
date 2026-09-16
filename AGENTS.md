# pglight — Project Rules (frontend + backend)

> Rules for every contributor (human or agent). Follow them; if a rule blocks
> you, say so instead of silently breaking it.

## 0. Stack map

- **Backend**: Go 1.25, `github.com/jackc/pgx/v5`. API in `internal/api/`
  (domain files: `session|explorer|query|data|admin|alter|complete|settings|aliases.go`,
  shared core in `handlers.go`), connection pool + explicit-txn state in
  `internal/db/manager.go`, routes in `main.go` (grouped by domain).
- **Frontend**: React 18 + Vite 5 + Tailwind v3 + shadcn-style prebuilt
  components (`web/src/components/ui/*`, Radix + cva + tailwind-merge +
  lucide-react) + Sonner toasts + promise-based dialog host (`dialogs.tsx`).
  Build output `web/dist` is **git-ignored** (local build artifact) — `main.go` embeds it.
- **Test DB**: `docker/` → postgres:14-alpine + `init.sql` seed.

## 1. Golden commands

```sh
go run ./scripts/rerun              # kill :8080, rebuild, rerun (PORT=… to change)
docker compose -f docker/docker-compose.yml up -d   # test DB
cd web && npm run dev              # Vite :5173, proxies /api → :8080
```

Gate before finishing any change: `go run ./scripts/check` (gofmt, vet, build,
  tsc, eslint). Frontend change ⇒ `cd web && npm run build` so the local `dist/`
  stays fresh (`dist/` is git-ignored: rebuild it, never commit it).

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
   - `/api/import` caps 20k rows, batches of 500, atomic (own txn unless the
     session already has one).
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
    owning its handler methods + request/response types + process-wide store
    (e.g. `complete_cache.go`, `aliases.go`). `handlers.go` owns only the shared kernel
    (`Handler`, `writeJSON`, `sessionID`/`sessionFromBody`, `q`/`pool`, `queryJSON`/`execQuery`,
    `rowsToMaps`, `errLocation`, cross-domain detectors like `ddlRe`). NEVER copy kernel
    helpers into a domain file; NEVER touch another domain's unexported state — use its
    exported API (`globalComplete.Invalidate`, `globalAliases.List`). No `misc.go`/`utils.go` catch-alls.

## 3. Frontend rules (React + shadcn)

1. **Prebuilt UI only**: compose from `components/ui/*` (Button, Input,
   Textarea, Badge, Card, Table, Tabs, Dialog, AlertDialog, Select, Separator,
   ScrollArea, Collapsible, DataGrid) + `feedback.tsx` (Skeleton/ErrorText/
   EmptyNote). Do NOT hand-roll buttons/inputs/modals/tables, do NOT add raw
   CSS files — style via `cn()` + Tailwind theme tokens. New generic widget?
   Add it to `ui/` in shadcn style (cva variants, Radix under the hood).
2. **No native dialogs, ever**: no `alert/confirm/prompt` —
   - notifications → `toast.success/error/info` (Sonner; monochrome thin style
     on `<Toaster>` in `App.tsx`, don't restyle per-call),
   - confirmations → `dialogs.confirm({title, description, confirmText, danger})`,
   - single input → `dialogs.prompt({title, description, defaultValue})`,
   - multi-field input → `dialogs.form({title, fields[]})` (one dialog, not a
     prompt loop). `dialogs` comes from `createDialogs` in `App.tsx`; pass it
     down as a prop (`dialogs: DialogsApi`).
3. **Icons**: `lucide-react` only, no emoji in UI chrome.
4. **Data access**: all HTTP via `lib/api.ts` (`api`/`apiClient`/`q(session,…)`).
   No `fetch` elsewhere. LocalStorage only via `useLocalStorage` (or the
   `useLocalStorage` hook in `lib/storage.ts`). Exception: ERD node layout +
   viewport persist raw via `components/erd/erdStorage.ts` (keys
   `pglight-erd-layout` / `pglight-erd-viewport`, session+schema scoped).
5. **State**: tabs are immutable (`setTabs(prev => prev.map(…))`, narrow by
   `t.kind`); per-tab loaders are `loadTablePage`/`loadTableMeta`-style
   functions in `App.tsx`. Feature components stay presentational: props in,
   callbacks out — no cross-component imports.
6. **Grids**: result/object tables use `<DataGrid>` or `ui/table` parts; cell
   values truncate (`max-w-[320px]`), NULL renders italic.
7. **Tooltips — one global style**: always `<Tip>` (`components/ui/tooltip.tsx`,
   dark `bg-popover` card). NEVER native `title=` (OS white box) on any element —
   dense cells/rows (DataGrid, TableWorkspace, ERD nodes) wrap truncated content
   with `<Tip>` too; CodeMirror autocomplete inherits `--popover` tokens.
8. **TS strict**: no `any`, no unused imports/locals, `@/` path alias (kept in
   sync in `tsconfig.json` + `vite.config.ts`). ESLint must pass;
   `eslint-disable` needs a one-line reason comment, never a blanket disable.
9. **New view checklist**: component in `components/` → wire into `App.tsx`
   tabs/side/explorer → reuse `ui/*` + `dialogs`/`toast` → `npm run build`.

## 4. Test DB rules

- `docker/init.sql` must stay re-runnable from scratch (`down -v` reseeds).
  It must cover every explorer group (table+FK, view, matview, function,
  sequence default, enum type, trigger, index, check/unique).
- `docker/sample-authors.csv` stays in sync with the `authors` table for
  import testing. Never commit real credentials — the `postgres/postgres`
  test-only login lives in the compose file, nowhere else.

## 5. Docs & commits

- New endpoint/component/behavior ⇒ update `PLAN.md` (API list / stack section).
- Never commit `web/dist` (git-ignored build output), `web/node_modules`, `*.tsbuildinfo`, `data/`, binaries, or `.env` files.
- **One feature = one commit.** Finish order: implement → `go run ./scripts/check`
  → rebuild `dist` → smoke-test (API + UI) → commit immediately. Message in
  conventional style: `feat|fix|docs|chore|refactor(scope): subject`, e.g.
  `feat(observability): AOP request/query logging with web settings`.
