# pglight — DataGrip / pgAdmin parity plan

Source features surveyed: JetBrains DataGrip (explorer, consoles, diff, Explain, data editor, import/export, diagrams, search) and pgAdmin 4 (browser tree, dashboard, Query Tool, Properties panels, maintenance, backup, roles).

## Where pglight is now (v0.1)

| Area | Status |
|---|---|
| Connect + saved servers (localStorage) | ✅ |
| Explorer: schemas → tables + est.rows, filter | ✅ basic |
| Table browse: WHERE filter, ORDER, paging | ✅ |
| Cell edit (prompt), insert (prompt), delete | ✅ basic |
| Query console: run, Ctrl+Enter, limit, history | ✅ basic |
| EXPLAIN (JSON) + text plan | ✅ basic |
| DDL (reconstructed) + indexes + FKs | ✅ basic |
| CSV/JSON export of result | ✅ |
| Activity (pg_stat_activity) + cancel/kill | ✅ |
| Autocomplete (tables/columns/keywords) | ✅ smart (context-aware, cached) |
| Multi-statement / multi-result | ❌ |
| Transactions (BEGIN/COMMIT/ROLLBACK) | ❌ |
| Full tree (views/matviews/foreign/functions/seq/types/triggers/extensions/roles) | ❌ partial |
| Properties (constraints/triggers/stats/sizes/comments) | ❌ partial |
| Dashboard (locks, db stats, server info) | ❌ partial (activity only) |
| Global object search (Ctrl+K) | ❌ |
| SQL format + highlight + snippets | ❌ |
| Import CSV / export as INSERTs | ❌ (export CSV/JSON only) |
| ER diagram (FK graph) | ❌ |
| Maintenance (VACUUM/ANALYZE/REINDEX) | ❌ |
| Roles / privileges viewer | ❌ |
| Diff / schema compare, backup/restore | ❌ (later) |
| Debugger, SSH tunnel | ❌ (out of scope) |

## Plan — phased

### Phase 1 — parity core (THIS CHANGE)
Goal: cover 80% of daily DataGrip/pgAdmin use without new deps.

Backend (`internal/db`, `internal/api`):
- [x] Per-session explicit transactions: `POST /api/txn {begin,commit,rollback,status}`. All query paths become txn-aware (see uncommitted data inside txn).
- [x] Multi-statement execution: `POST /api/query` detects `;`-separated statements (quote/dollar-quote/comment aware) and returns `results[]`. Single-statement shape kept for backwards compat.
- [x] `GET /api/server-info` — version, uptime, current db/size, settings snapshot, connection counts.
- [x] `GET /api/stats` — per-database stats + top tables by size + cache hit ratio.
- [x] `GET /api/locks` — `pg_locks ⋈ pg_stat_activity`, blockers first.
- [x] `GET /api/roles` — roles, superuser/login, member-of.
- [x] `GET /api/extensions` — installed + available.
- [x] `GET /api/types?schema=` — enums, domains, composites with definitions.
- [x] `GET /api/triggers?schema=&table=` and `GET /api/constraints?schema=&table=`.
- [x] `GET /api/view-def`, `GET /api/func-def` — real `pg_get_viewdef / pg_get_functiondef`.
- [x] `GET /api/table-stats` — sizes, seq/idx scans, live/dead tuples, vacuum/analyze ages.
- [x] `GET /api/erd?schema=` — nodes + FK edges for diagram.
- [x] `GET /api/search?q=` — global object search (tables/views/functions/columns, ILIKE, 100 rows cap).
- [x] `GET /api/complete?session_id=` — structured schema snapshot for autocomplete: `{version, tables[{schema,name,columns[{name,type}]}], functions[{schema,name,args}], fks[{src,dst}], keywords, in_txn}`. Per-session cache (60s TTL, single-flighted); `ETag`/`If-None-Match` → 304; `?refresh=1` bypasses; auto-invalidated after DDL via `/api/query` and on disconnect.
- [x] `POST /api/maintenance {vacuum,analyze,vacuum_full,reindex}` — pgAdmin-style maintenance buttons.
- [x] `POST /api/import {columns, rows[][]}` — txn-wrapped bulk INSERT for CSV import (500-row batches, `ON CONFLICT DO NOTHING` option).

Frontend (`web/`):
- [x] Explorer: Databases node + per-schema groups (Tables/Views/MatViews/Foreign/Functions/Sequences/Types) + global Extensions/Roles nodes. Lazy per-schema loading, filter, counts. Right-click context menus (Radix `ui/context-menu`) at database/schema/table tiers: scoped New Query (`SET search_path` + `SELECT … LIMIT 100`), New Schema/Table dialogs, Open Data, View Definition, Export CSV/INSERTs (≤1000 rows), Copy name, Refresh. No new backend endpoints.
- [x] Table workspace with sub-tabs: **Data | Columns | DDL | Indexes | Constraints | Triggers | Stats**. DDL uses server definition + reconstructed fallback.
- [x] Query console upgrades: txn bar (autocommit toggle, Begin/Commit/Rollback, in-txn badge), Format button, Save-snippet, multi-result rendering (one grid per statement), per-result CSV/INSERT export. Statement timeout 1000s (`queryTimeout` in `internal/api/handlers.go`, console paths only). Cancel button (■): matches the tab's pool via `application_name=pglight:<session>` (`Manager.Add`) against active backends, SQL text only disambiguates concurrent runs; unique hit → `GET /api/cancel`, else toast pointing to Dashboard.
- [x] Global search palette (Ctrl+K / button): jump to table/view/function, open DDL or data.
- [x] Dashboard panel: Server | Activity | Locks | Stats tabs (auto-refresh activity/locks).
- [x] ERD tab per schema: SVG FK graph (click node → open table).
- [x] Import CSV into open table (file picker, header detection, batch POST), Export as INSERT statements, copy cell.
- [x] Snippets library (localStorage) + searchable history.
- [x] SQL keyword formatter + smart autocomplete (CodeMirror 6, PostgreSQL dialect): context-aware (FROM/JOIN → tables, SELECT/WHERE/ON → scoped columns, `alias.` → that table's columns), ranking prefix > word-boundary > substring > fuzzy + FROM-scope/FK-neighbour/alias/recent-use boosts, SQL snippets, function signatures, Ctrl+Space manual trigger, recent-use in localStorage. Snapshot cached: 1 fetch/session/60s (memory-first, SWR, ETag), zero network per keystroke; refresh button in console; Run-selection (Ctrl+Enter) and panel resize unchanged.

### Phase 2 — next (not in this change)
- Visual EXPLAIN (flame/graph, buffers/timing bars), plan compare.
- Data editor: staged edits + Apply/Rollback (like DataGrip), JSON cell editor, column filters/sort UI, duplicate row, fill-down.
- Schema diff (DDL compare between DBs), migration script preview.
- `pg_dump`/`pg_restore` streaming backup, MOTD log viewer (`pg_log` via `file_fdw` if available).
- Row-level security / privilege editor (GRANT wizard), role membership editor.
- Charts from result sets, query plan history, slow-query panel (`pg_stat_statements` when installed).
- Multi-connection tabs (per-tab session) ✅ (sessions list + active session; explorer/txn/dashboard follow active, tabs keep their session; `switchDb` opens a new session) | SSH tunnel + SSL cert auth, read-only mode (later).

## API added in Phase 1

```
POST /api/txn              {session_id, action}
GET  /api/sessions          → {sessions: [{id,host,port,user,dbname,sslmode,in_txn,connected_at}]} (display info only, no passwords)
POST /api/connect           (now also returns {info} alongside session_id, additive)
GET  /api/server-info?session_id=
GET  /api/stats?session_id=
GET  /api/locks?session_id=
GET  /api/roles?session_id=
GET  /api/extensions?session_id=
GET  /api/types?session_id=&schema=
GET  /api/triggers?session_id=&schema=&table=
GET  /api/constraints?session_id=&schema=&table=
GET  /api/view-def?session_id=&schema=&name=
GET  /api/func-def?session_id=&schema=&name=
GET  /api/table-stats?session_id=&schema=&table=
GET  /api/erd?session_id=&schema=
GET  /api/search?session_id=&q=
POST /api/maintenance       {session_id,schema,table,op}
POST /api/import            {session_id,schema,table,columns,rows,on_conflict_do_nothing}
POST /api/query             (now multi-statement aware → {results[]} when >1)
GET  /api/settings          → {logging: {enabled,level,log_http,log_query,slow_ms,max_entries}}
POST /api/settings          {logging: {...}} (normalized + persisted to data/logging.json)
GET  /api/logs?limit=&level=&category= → {entries[]} (newest first; /api/logs not self-logged)
DELETE /api/logs            clear the ring buffer
```

All txn-aware: `/api/query`, `/api/explain`, `/api/table-data`, `/api/row`, `/api/import`.

## Frontend stack (Phase 2 — shadcn)

The hand-rolled vanilla UI (`web/app.js` + `web/style.css`) was replaced with a
React + Vite + Tailwind v3 app built on **shadcn-style prebuilt components**
(`web/src/components/ui/*`: Button, Input, Textarea, Badge, Card, Table,
Tabs, Dialog, Select, Separator, ScrollArea, Collapsible, Switch, Resizable
(react-resizable-panels, persisted via autoSaveId) for the 3-column layout,
DataGrid) on top of Radix
primitives, `class-variance-authority`, `clsx` + `tailwind-merge`,
`lucide-react` icons, Sonner `<Toaster>` (monochrome thin style) for all
notifications, and a promise-based `dialogs.tsx` host on top of shadcn
`AlertDialog`/`Dialog` for every confirmation and input — no native
`alert()`/`confirm()`/`prompt()` anywhere — instead of reimplemented
widgets, to avoid custom-CSS and bespoke-component bugs.

Feature views: slim `ConnectionBar` (status/badges/actions + Settings gear) +
`SettingsPanel` (logging config: enabled/level/http/query/slow-threshold/max
+ live log viewer with level/category filters, auto-refresh, clear) +
`CredentialManager` (saved servers + credential form in a collapsible Radix
`Collapsible` panel docked above the explorer; auto-collapses on connect,
reopens on disconnect, state persisted), `Explorer` (databases → schemas →
tables/views/matviews/foreign/functions/sequences/types + server objects),
`QueryConsole` (multi-result, EXPLAIN text plan, formatter, per-result
CSV/JSON/INSERT export), `TableWorkspace` (Data/Columns/DDL/Indexes/
Constraints/Triggers/Stats sub-tabs, cell edit/duplicate/delete, CSV import,
VACUUM/ANALYZE/REINDEX), `BrowserView` (extensions/roles), `ErdView` (FK
graph SVG), `SidePanel` (history/snippets/server/activity/locks/stats with
auto-refresh), `SearchPalette` (Ctrl+K global search dialog), last-session restore (open tabs +
active tab + autocommit persist to localStorage; autologin from last successful
connection with an opt-out Switch in Connections).

Dev workflow:

```sh
cd web && npm install   # once
npm run dev             # :5173, proxies /api → Go on :8080
npm run build           # emits web/dist (committed so `go build` works)
```

`main.go` embeds `web/dist` and serves it with an SPA fallback to
`index.html`. Rebuild the bundle (`npm run build` in `web/`) after any
frontend change before `go build`.

Conventions for contributors live in `AGENTS.md` (backend + frontend rules,
checklists); run `go run ./scripts/check` before finishing any change.

Restart the backend (kills `:8080`, rebuilds, reruns in background):

```sh
go run ./scripts/rerun          # PORT=8080 default
PORT=18080 go run ./scripts/rerun
```

## Test database (docker/postgres:14-alpine)

```sh
docker compose -f docker/docker-compose.yml up -d   # pg14 + seed demo data
docker compose -f docker/docker-compose.yml down    # stop (keeps data)
docker compose -f docker/docker-compose.yml down -v # reset + reseed
```

Preconfigured credentials (match the UI defaults, just type the password):
`host=localhost port=5432 user=postgres password=postgres dbname=postgres`.

`docker/init.sql` seeds tables with FK (`authors` → `books` → `reviews`),
a view (`published_books`), matview (`author_stats`), function
(`book_count_by_status`), enum (`mood`), trigger (`trg_books_touch`) and
indexes — enough to exercise explorer groups, ERD, constraints, stats and
multi-result queries. Verified end-to-end 2026-09-10: connect, schemas,
tables, all 8 object kinds, columns, DDL/view/func defs, constraints,
triggers, table-stats, single + multi-statement query, EXPLAIN, txn
begin→insert→rollback visibility, WHERE-less update refusal, server-info,
stats, locks, activity, roles, extensions, complete, import, ANALYZE.
Safety kept: UPDATE/DELETE without WHERE still refused; maintenance allow-lists ops.
