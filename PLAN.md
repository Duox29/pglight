# pglight — DataGrip / pgAdmin parity plan

Source features surveyed: JetBrains DataGrip (explorer, consoles, diff, Explain, data editor, import/export, diagrams, search) and pgAdmin 4 (browser tree, dashboard, Query Tool, Properties panels, maintenance, backup, roles).

## Where pglight is now (v0.2, `web` 0.2.0)

| Area | Status |
|---|---|
| Connect + saved servers (sqlite profiles + legacy migration) | ✅ |
| Explorer: schemas → tables + est.rows, filter | ✅ |
| Table browse: WHERE filter, ORDER, paging | ✅ |
| Cell edit, insert, guarded delete | ✅ (PK-scoped, `single:true`, bulk `rows-delete`) |
| Query console: run, Ctrl+Enter, limit, history | ✅ (limits 200/1k/5k/10k/no-limit + 64 MiB guard) |
| EXPLAIN (JSON) + text plan | ✅ |
| DDL + indexes + FKs | ✅ (OID-joined, `to_regclass` quoted names) |
| CSV/JSON export of result | ✅ (+ INSERT export, OID exact numerics) |
| Activity (pg_stat_activity) + cancel/kill | ✅ (`application_name` attribution) |
| Autocomplete (tables/columns/keywords) | ✅ smart (context-aware, cached) |
| Multi-statement / multi-result | ✅ (`results[]`, per-statement error locations) |
| Transactions (BEGIN/COMMIT/ROLLBACK) | ✅ (serial querier + lease + 15min sweeper) |
| Full tree (views/matviews/foreign/functions/seq/types/triggers/extensions/roles) | ✅ |
| Properties (constraints/triggers/stats/sizes/comments) | ✅ |
| Workspace (server, activity, locks, db stats) | ✅ (Workspace tab: Server/Activity/Locks/Stats) |
| Global object search (Ctrl+K) | ✅ |
| SQL format + CodeMirror editor + snippets | ✅ (sqlite-backed snippets/history) |
| Import CSV / export as INSERTs | ✅ (20k cap, 500-row batches, atomic) |
| ER diagram (FK graph) | ✅ (`@xyflow/react`, auto-layout, persisted) |
| Maintenance (VACUUM/ANALYZE/REINDEX) | ✅ (allow-listed, txn-refused) |
| Roles / privileges viewer | ✅ basic |
| Mock-data generator (Simple/Advanced) | ✅ (`meta`/preview≤100/generate≤20000) |
| Split workspace (2-pane) + Docs tab + shutdown | ✅ |
| Privacy controls + TLS warnings + exact numerics | ✅ |
| Diff / schema compare, backup/restore | ❌ (later) |
| Debugger, SSH tunnel | ❌ (out of scope) |

## Plan — phased

### Phase 1 — parity core (shipped, v0.2)
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
- [x] UX hierarchy pass: header reduces visual competition (search-first + configurable Quick Access buttons, with History/Dashboard as defaults + utility menu for Workspace/Docs); query toolbar grouped Primary/Query/Result/Utility with Run dominant and Explain in a dropdown; connection panel is a floating Radix Popover over the explorer (never pushes the tree down); Explorer tiers schema > muted-uppercase group > object, Tables open by default, Lucide-only icons with subtle type colors, filter match counts; tabs show active dot/dirty state, middle-click close, DB badges; DataGrid is type-aware (OID-based numeric right-align, bool, JSON, type tooltips, zebra rows, Lucide sort icons); table workspace has a real identity header with Data | Structure (Columns/Constraints/Triggers) | SQL | Indexes | Stats; side-panel tools moved into one persisted Workspace tab with a single navigation bar and Quick Access configuration …
- [x] Table workspace with sections **Data | Structure (Columns/Constraints/Triggers) | SQL (DDL) | Indexes | Stats**. DDL uses server definition + reconstructed fallback. Columns tab edits via `POST /api/alter-table`: add/drop/rename column, change type (smart-suggest input, custom enums accepted), toggle nullable, set/drop default, rename table; Constraints tab adds/drops CHECK|UNIQUE|PK|FK|EXCLUDE; Indexes tab creates (unique, btree|hash|gin|gist|spgist|brin, multi-key ASC/DESC, expression keys, INCLUDE, partial WHERE, live SQL preview)/renames/drops; Triggers tab creates (BEFORE|AFTER|INSTEAD OF, INSERT|UPDATE|DELETE|TRUNCATE, ROW|STATEMENT, function, UPDATE OF cols, WHEN)/enables/disables/drops with status badge (`GET /api/triggers` now also returns `enabled` O|D|R|A).
- [x] Query console upgrades: txn controls live inside each tab's own toolbar (per-tab session: `user@host/dbname` label, autocommit toggle, Begin/Commit/Rollback, open/no-transaction badge) — no separate global bar, so switching tabs can never act on the wrong session; Format button, Save-snippet, multi-result rendering (one grid per statement), per-result CSV/INSERT export. Statement timeout 120s (`queryTimeout` in `internal/api/handlers.go`, console paths only). Cancel button (■): matches the tab's pool via `application_name=pglight:<session>` (`Manager.Add`) against active backends, SQL text only disambiguates concurrent runs; unique hit → `GET /api/cancel`, else toast pointing to Dashboard.
- [x] Global search palette (Ctrl+K / button): jump to table/column, open data.
- [x] Session survive-restart: per-session credentials (`session-conns`), boot 1:1 reconnect so tabs keep their own DB (dead sessions badged, never collapsed onto another DB), global 401 hook + 30s/focus heartbeat with one-shot auto-retry, per-session Reconnect / Reconnect-all in Connections, tab ids remapped on reconnect.
- [x] Workspace tab: History | Snippets | Aliases | Server | Activity | Locks | Stats | Settings | Shortcuts | Logs, with one navigation bar; Quick Access uses a per-section toggle list for Connection Bar visibility (History/Dashboard by default) and database views retain auto-refresh for Activity/Locks.
- [x] ERD tab per schema: SVG FK graph (click node → open table).
- [x] Import CSV into open table (file picker, `.tsv` forced to tab delimiter + header detection + ragged-width reject, batch POST), Export as INSERT statements (identifier-quoted, typed literals: NULL/TRUE/FALSE/numbers/JSON/arrays), right-click opens the row menu (Copy cell value / Export / Copy / Delete — never `preventDefault` on the cell, or Radix skips open). CSV export quotes headers, NULL as empty, objects as JSON.
- [x] Grid selection flow (shared `useGridSelection`, Open Data + query `DataGrid[selectable]`): plain left-click selects exactly one row, ctrl/meta toggles, shift ranges from anchor, right-click keeps multi-selection when inside it; query grids offer Copy cell value / Copy rows / Export selected CSV|JSON.
- [x] Async safety: `api()` throws `ApiError` on transport/invalid-JSON (backend `{error}` JSON still resolved per contract); `runQuery` uses try/finally so the running flag always clears; txn/explorer/table/row/alter/cancel/maintenance/import paths surface transport errors via toast or tab error; reconnect-all is per-session guarded; browser/ERD/restore loads carry a token so late responses cannot overwrite newer tabs; search palette drops stale responses by sequence.
- [x] Row identity: cell edit uses the table's primary key from `/api/columns` and refuses when there is none (or a PK value is NULL); bulk selection keys by PK values when known, falling back to row-index-plus-content keys; pagination disables Prev at offset 0 and honors the backend `has_more` signal; query result sort is derived state with asc/desc instead of mutating tab results.

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
POST /api/connect           {host,port,user,password,dbname,sslmode} → {session_id,info,tls_warn}
GET  /api/sessions          → {sessions: [{id,host,port,user,dbname,sslmode,in_txn,connected_at}]} (display info only, no passwords)
GET  /api/disconnect?session_id=
POST /api/txn              {session_id, action}
GET  /api/databases?session_id= | GET /api/schemas?session_id= | GET /api/tables?session_id=&schema=
GET  /api/objects?session_id=&schema= (views/matviews/foreign/functions/sequences/types)
GET  /api/columns?session_id=&schema=&table= | GET /api/ddl?session_id=&schema=&table=
GET  /api/table-data?session_id=&schema=&table=&limit=&offset=&filter=&order= → {columns,types,rows,total,has_more,in_txn}
POST /api/query             {session_id,sql,limit} (multi-statement → {results[]}; see below)
POST /api/explain           {session_id,sql} → EXPLAIN ANALYZE (JSON, buffers)
GET  /api/activity?session_id= | GET /api/cancel?session_id=&pid=[&kill=1]
POST /api/row               {session_id,schema,table,op,values,where[,single]} (`single:true` verifies exactly 1 affected row, 409 otherwise)
POST /api/rows-delete       {session_id,schema,table,where[]} → {deleted} — atomic bulk delete (one txn; every entry must match exactly 1 row)
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
GET  /api/seq-def?session_id=&schema=&name=   → {schema,name,data_type,start_value,minimum_value,maximum_value,increment,cycle_option,cache_size,last_value,definition}
GET  /api/type-def?session_id=&schema=&name=  → {schema,name,kind,comment,labels,definition,display} (definition set for enums; display=format_type for the rest)
GET  /api/table-stats?session_id=&schema=&table=
GET  /api/erd?session_id=&schema=  (also returns `columns: {table: [{name,type,pk,fk}]}` for the ERD canvas — additive, nodes/edges unchanged)
GET  /api/search?session_id=&q=
POST /api/maintenance       {session_id,schema,table,op}
POST /api/import            {session_id,schema,table,columns,rows,on_conflict_do_nothing}
POST /api/alter-table       {session_id,schema,table,op,…} — columns: add_column|drop_column|rename_column|alter_type|set_nullable|set_default|rename_table (types validated via to_regtype, custom enums ok); constraints: add_constraint|drop_constraint; indexes: create_index{index?,unique,method,columns[],include[],where}|drop_index|rename_index (CREATE INDEX takes an unqualified name — always lands in the table's schema); triggers: create_trigger{trigger,timing,events[],for_each,function,update_of[],when}|drop_trigger|enable_trigger|disable_trigger
Object tabs (functions/sequences/types): view definition + properties; edits run through `POST /api/query` with quoted identifiers — sequence ALTER (increment/min/max/cache/restart/cycle), function CREATE OR REPLACE, enum ADD VALUE, sequence/type RENAME, DROP (functions resolved via `regprocedure`, all overloads confirmed). No new backend endpoint.
Multi-statement: `POST /api/query` returns `{results[]}` when >1 statement; console limits 200/1000/5000/10000 rows or no limit; no-limit results guarded at 64 MiB with an actionable 413 error; large grids virtualize DOM rows.
GET  /api/settings          → {logging: {enabled,level,log_http,log_query,slow_ms,max_entries}}
POST /api/settings          {logging: {...}} (normalized + persisted to data/logging.json)
GET  /api/logs?limit=&level=&category= → {entries[]} (newest first; /api/logs not self-logged)
DELETE /api/logs            clear the ring buffer
GET  /api/aliases           → {aliases: [{trigger,expansion,detail?,builtin}]} (builtins merged with user overrides; global, no session/PG)
POST /api/aliases           {trigger,expansion} → upsert user entry (trigger: [a-z][a-z0-9_]{0,31}, expansion ≤2000 chars; shadows builtin; 200-entry cap)
DELETE /api/aliases?trigger= drop one user entry (builtin restored) · DELETE /api/aliases reset all to defaults
GET  /api/snippets | POST /api/snippets {name,sql} | DELETE /api/snippets?name= (sqlite-backed, per user)
GET  /api/history | POST /api/history {sql,ms?,n?} | DELETE /api/history (sqlite-backed, retention-pruned)
GET  /api/connections | POST /api/connections {name,host,port,user,dbname,sslmode} | DELETE /api/connections?name= (saved profiles, no passwords stored client-side)
GET  /api/preferences | POST /api/preferences {…} (per-user JSON blob: autocommit, privacy, layout)
GET  /api/preferences/shortcuts | PUT /api/preferences/shortcuts {version,overrides} (validated application-wide command-key overrides)
GET  /api/complete?session_id=[&refresh=1] (+ ETag/If-None-Match → 304; auto-invalidated after DDL/disconnect)
POST /api/shutdown          → {ok:true} — close all session pools, then stop the server (header Power button, confirm dialog; POST-only, 405 otherwise; reply flushes 200ms before `os.Exit`, so it is never invoked by tests — `Manager.CloseAll` is covered in `db_test.go` instead)
GET  /api/mock-data/meta?session_id=&schema=&table= → {columns[{name,data_type,udt,nullable,default,identity,generated,primary_key,unique,enum_values,semantic_hint}],foreign_keys[],checks[{name,definition,kind}],composite_uniques?,has_partial_unique?} — normalized introspection (catalogs/information_schema; React never parses DDL). Composite UNIQUE members are NOT flagged per-column unique; composite/partial uniques surface as planner warnings (insert-validated, not pre-satisfied).
POST /api/mock-data/preview  {session_id,schema,table,mode,count≤100,seed?,fields?,constraints?} → {columns,rows,warnings,seed,in_txn} — generates without touching the DB; DB-filled columns render as `<database default>`
POST /api/mock-data/generate {session_id,schema,table,mode,count≤20000,seed?,fields?,constraints?} → {generated,inserted,seed,duration_ms,in_txn} — atomic bulk insert via the shared `insertRowsBatched` helper (batches sized for the 65535-parameter limit; zero-column plans batch DEFAULT VALUES statements; single-column FK pools ORDER BY for seed determinism). Txn-aware via the `Manager.AcquireLease` operation lease: when an explicit txn is open the raw pgx.Tx is held under e.mu for metadata → generation → ALL batches, so a racing Commit blocks until release instead of partially committing. Advanced validation: exact integer params (json.Number-safe, int2/int4/int8 bounds, min≤max for integer/decimal/relative offsets), strict full-consumption CHECK grammar (any unconsumed predicate → unsupported + warning), composite FKs treated as one unit (any per-member override/custom generator is a 400), weights capped/normalized once at plan time, constant/choice payloads capped, sequence arithmetic overflow-checked, large day offsets via AddDate. Uniqueness is batch-scoped (warned); compare literals accept json.Number (no lexicographic fallback).
```

Mock-data hardening: CHECK integer bounds and custom numeric comparisons retain
BIGINT precision (no float64 round-trip); strict decimal CHECKs fail closed.
Sequence generation validates the complete range against int2/int4/int8 width,
weights are normalized once per plan, and MATCH FULL composite foreign keys
make NULL decisions at tuple level.

Mock Data Generator (Table workspace → Generate → `MockDataDialog`):
Simple = datatype-only, zero-config (random strings for `email`/`first_name`
— no semantic inference, no CHECK/UNIQUE/FK intelligence; identity/generated/
serial/default columns omitted for PostgreSQL to fill; seeded runs are
deterministic via a seed-anchored clock, unseeded runs use wall time).
PostgreSQL stays the final validator — insertion failures return the PG error
plus "Use Advanced mode to configure constraints" in Simple mode only
(advanced errors stay raw).
Advanced = schema-aware: semantic Auto generators (Email/Name/Phone/URL…),
numeric/date ranges, Choice/Sequence/Constant/JSON/Array, NULL probability
(clamped 0..1 server-side; NULLs bypass uniqueness tracking, matching PG),
UNIQUE (single-column constraints/indexes only — composite UNIQUEs never mark
members; single-key rule uses `indnkeyatts=1` and skips partial/expression
indexes; bounded 50-attempt retry, never infinite), existing-row FK pools
(uniform/sequential round-robin; composite FOREIGN KEYs pick one referenced
tuple per row so members can't mix into nonexistent combinations; unknown
mode falls back to uniform; empty non-nullable FK errors; per-field source
picker overrides the pool via params `ref_schema/ref_table/ref_column`,
identifiers sanitized, same-session read so uncommitted parents are visible),
conservative CHECK inference (OR/NOT/CASE/functions/multi-column expressions
are unsupported and warn instead of partially inferring; BETWEEN/comparison/IN
forms incl. PG-normalized `>= AND <=` and `= ANY (ARRAY[…])` with `::type`
casts stripped and `''` unescaped; multiple checks on one column merge to the
tightest bound; inferred ranges feed integer AND decimal generators, IN sets
feed integer/string/choice; explicit params win), enum columns resolve Auto
to Choice over their labels, IN-checks narrow Auto strings, Auto on
identity/serial resolves to DB Default, custom compare constraints
(`= != < <= > >=` plus `<>`, structured JSON only — no expression eval,
max 64 per request; literals allowed as sides; NULLs compare only via
`=`/`!=`), and Relative DateTime dependents ordered by a dependency graph
(cycles rejected; missing source, unknown source and default-omitted source
all error clearly). Default-only tables (all identity/default) generate
DEFAULT rows instead of erroring. Sequence/FK round-robin counters commit
only on successful rows, so constraint retries leave no gaps.
Request bodies decode with `Decoder.UseNumber()`, so numeric params arrive as
`json.Number` — the engine coerces them (ints, floats, weights, scales).
Advanced params are validated server-side at plan time (string lengths ≤4096,
array items ≤1000, choices ≤1000 values, finite numbers, offsets ≤365000
days, true_probability ∈ [0,1]); JSON bodies are capped at 64MB.
Engine is pure/deterministic under `internal/mockgen/` (schema, generators,
constraints, generator); HTTP orchestration in `internal/api/mockdata.go`
(preview ≤100 rows, generate ≤20000, both txn-aware; FK pools load once per
request and feed random + sequential modes).
```

Wire-format policy (exact numerics): int8/numeric/money (and their array

Wire-format policy (exact numerics): int8/numeric/money (and their array
variants) cross JSON as **strings** with the type OID alongside
(`jsonSafeCells` in `handlers.go`); request bodies decode with
`Decoder.UseNumber()`, and `/api/row` + `/api/import` coerce digit-strings
against the real column `udt_name`, so bigint PKs survive the JS round-trip
exactly. `resultToInserts`/`quoteLiteral` understand OID type tags.

Txn concurrency: one explicit txn serializes all session operations through
a per-session mutex (`db.serialQuerier`); Commit/Rollback/Close wait for
in-flight work (lock order is always txnLock → e.mu → m.mu; the sweeper
copies entry refs under m.mu and releases it before touching e.mu, so no
m.mu→e.mu vs e.mu→m.mu deadlock; `done` is an atomic flag, race-clean).
Write paths (`/api/mock-data/*`, `/api/import`) hold a `Manager.AcquireLease`
operation lease for the whole metadata → generation → all-batches sequence,
so a racing Commit blocks until release instead of partially committing.
Transactions idle >15min are rolled back by the
sweeper (surfaced via `in_txn: false` on next status poll).

Catalog hardening: foreign-table filter uses `foreign_table_schema`;
all `(schema||'.'||table)::regclass` lookups use
`to_regclass(format('%I.%I', …))` (quoted/case/dotted names work); ERD and
PK detection join `pg_constraint/pg_class/pg_attribute` by OID (duplicate
constraint names can no longer cross-link). DSN is built from URL parts
(spaces/IPv6 safe); sslmode allow-list
`disable|prefer|require|verify-ca|verify-full` (default `prefer`);
`sessions`/`connect` report `tls_warn` for unverified non-loopback links and
the Connections panel warns visibly. App data is private: store/logging
dirs 0700, files 0600; Settings → Privacy controls history persistence,
result-snapshot restore (default off), retention days (server-enforced
prune), and clear-history / clear-all-local-data. NULL is real JSON null
with Set-NULL controls — the `__NULL__` sentinel is gone (literal text
stores verbatim).

All txn-aware: `/api/query`, `/api/explain`, `/api/table-data`, `/api/row`, `/api/rows-delete`, `/api/import`, `/api/alter-table`.

## Frontend stack (Phase 2 — shadcn)

The hand-rolled vanilla UI (`web/app.js` + `web/style.css`) was replaced with a
React + Vite + Tailwind v3 app built on **shadcn-style prebuilt components**
(`web/src/components/ui/*`: Button, Input, Textarea, Badge, Card, Table,
Tabs, Dialog, AlertDialog, Select, SearchSelect (searchable single-select: Radix Popover +
filter input + option list, for FK sources and other user-named lists — plain
Select has no search), Separator, ScrollArea (always mounts vertical +
horizontal bars; Radix only enables horizontal scrolling when the horizontal
bar is mounted, otherwise wide tables clip with no scrollbar), Collapsible,
Switch, Resizable
(react-resizable-panels, persisted via autoSaveId) for the 3-column layout,
ContextMenu, DropdownMenu, Popover, Tooltip/`Tip` (single global dark
`bg-popover` style — never native `title=`); app scrollbars use a fixed
overlay layer, stay transparent until their scroll container is hovered, and
support both axes; visibility is configurable as `hover|always|never` in
`web/src/lib/scrollbar.ts`,
DataGrid) on top of Radix
primitives, `class-variance-authority`, `clsx` + `tailwind-merge`,
`lucide-react` icons, Sonner `<Toaster>` (monochrome thin style) for all
notifications, and a promise-based `dialogs.tsx` host on top of shadcn
`AlertDialog`/`Dialog` for every confirmation and input — no native
`alert()`/`confirm()`/`prompt()` anywhere — instead of reimplemented
widgets, to avoid custom-CSS and bespoke-component bugs.

Feature views: slim `ConnectionBar` (status/badges/actions + Settings gear +
destructive Power button → `onShutdown`: confirm dialog, farewell toast, then
`POST /api/shutdown`) +
`SettingsPanel` (logging config: enabled/level/http/query/slow-threshold/max
+ live log viewer with level/category filters, auto-refresh, clear) +
`CredentialManager` (saved servers + credential form in a floating Radix
`Popover` panel over the explorer; auto-collapses on connect,
reopens on disconnect, state persisted), `Explorer` (databases → schemas →
tables/views/matviews/foreign/functions/sequences/types + server objects),
`QueryConsole` (multi-result, Explain Analyze text plan for whole or selected SQL, formatter, per-result
CSV/JSON/INSERT export), `TableWorkspace` (Data/Columns/DDL/Indexes/
Constraints/Triggers/Stats sub-tabs, cell edit/duplicate/delete, CSV import,
Generate mock data (`MockDataDialog`: Simple zero-config vs Advanced
per-column grid with Generator select + params, NULL % and Unique switch,
pk/fk/unique/not-null badges with ref-target tooltips, compare-constraint
builder, FK source `SearchSelect`, 20-row `DataGrid` preview with warnings,
Generate N Rows with client-side validation,
plus `ColumnEditor`/`IndexTriggerEditor` and row maintenance
`VACUUM/ANALYZE/REINDEX`), `BrowserView` (extensions/roles), `ObjectView` (function/sequence/type defs + edits via `/api/query`), `ErdView` (React Flow
FK canvas: `components/erd/` — column-level edges, FK-directed auto-layout (child-left/parent-right along edge arrows, longest-path layers, DFS cycle-break, barycenter ordering, isolated tables in one grid block, blocks shelf-packed; Auto arrange re-tidies + persists, Reset clears + rebuilds), drag persistence, viewport (pan/zoom) persistence per session+schema, search, high-contrast minimap with accent viewport frame), `Workspace` tab (history/snippets/aliases/server/activity/locks/stats/settings/shortcuts/logs with
one navigation bar, Quick Access preference, and auto-refresh), `AliasesPanel` + `LogsPanel` + `SettingsPanel` (logging config + privacy controls), `SearchPalette` (Ctrl+K global search dialog), `DocsView` (in-app docs tab), `TxnControls` (per-tab-tab autocommit/begin/commit/rollback badge), last-session restore (open tabs +
active tab + autocommit persist to localStorage; autologin from last successful
connection with an opt-out Switch in Connections). Workspace split view (max 2
panes over the single tabs array: `TabStrip` tab bar with Split Right/Down
context items, `SplitWorkspace` resizable 2-pane chrome with secondary tab
picker + direction/swap/close, `TabContent` per-tab composition root,
`hooks/useSplit` pin state (splitting pins the current tab left and opens the
adjacent tab right; flipping tabs in the strip then only swaps the right pane)
with direction persisted via `useAppPreference`).
`App.tsx` is a thin shell
(hook composition + 3-column layout + tab strip); domain logic lives in
`hooks/`: `useSessions` (multi-session connect/disconnect/reconnect/boot/
heartbeat + txn map + autocommit), `useTabs` (tab model, open/close/remap,
last-session restore), `useSplit` (2-pane split state), `useExplorer` (tree + schema/table actions),
`useQueryRunner` (run/cancel/explain + history/snippets), `useTableOps`
(table data/meta/row/alter), `useObjectOps` (function/sequence/type DDL),
`useGridSelection` (shared single/toggle/range + right-click selection);
shared pure helpers (`qi`, tab-id builders, `slimTab`/restore, `pkOf`, `DDL_RE`) in
`lib/tabs.ts`, SQL formatting in `lib/format.ts`, editor completion in
`lib/complete.ts` + `lib/schemaCache.ts`.
Dev workflow:

```sh
cd web && npm install   # once
npm run dev             # :5173, proxies /api → Go on :8080
npm test                # vitest (jsdom): hooks + components
npm run build           # emits web/dist (git-ignored; main.go embeds it, so always rebuild before go build)
```

`main.go` embeds `web/dist` and serves it with an SPA fallback to
`index.html`. Rebuild the bundle (`npm run build` in `web/`) after any
frontend change before `go build`.

## Application startup

The production binary accepts `--p`, `--port`, or `-p` for an explicit HTTP
port, plus `--no-browser` to suppress automatic browser opening. Configuration
priority is CLI flag, then `PORT`, then the default `8080`. Explicitly selected
ports are strict: a bind failure exits. With no explicit port, startup tries
`8080` and then successive ports for up to 100 attempts. On success the binary
opens `http://127.0.0.1:<actual-port>` through the platform default browser
(Windows `rundll32`, macOS `open`, Linux `xdg-open`); a browser-open failure is
only logged and does not stop the server. Vite development remains fixed at
the Go backend's `8080` proxy.

## CI & releases (`.github/workflows/build.yml`)

Every push to `master`, PR, and manual dispatch runs the gate (`gofmt`, `go
vet`, `go test`, `go test -race`, `govulncheck`, `tsc`, `eslint`, `npm test`
in `web/`) against a
postgres:14 service (integration tests run live; they skip without a DB)
plus a 9-target matrix build. Toolchain: `go 1.26.8` via `go-version-file`,
Node 22 via `setup-node` (`jsdom@30` needs it). Note: the local
`./scripts/check.sh` runs the fast subset (gofmt, vet, test, build, tsc,
eslint) — `-race`, `govulncheck`, and `npm test` are CI-only, run them
manually before risky changes.
Tagging `v*` (e.g. `git
tag v0.3.0 && git push origin v0.3.0`) additionally publishes a GitHub Release
with all archives attached:

| File | Target |
|---|---|
| `pglight-windows-386.zip` | Windows 32-bit (x86) |
| `pglight-windows-amd64.zip` | Windows 64-bit (x86-64) |
| `pglight-windows-arm64.zip` | Windows on ARM |
| `pglight-linux-386.tar.gz` | Linux 32-bit (x86) |
| `pglight-linux-amd64.tar.gz` | Linux 64-bit (x86-64) |
| `pglight-linux-armv7.tar.gz` | Linux ARMv7 (e.g. Raspberry Pi 32-bit) |
| `pglight-linux-arm64.tar.gz` | Linux ARM64 (e.g. Pi 4/5, AWS Graviton) |
| `pglight-darwin-amd64.tar.gz` | macOS Intel |
| `pglight-darwin-arm64.tar.gz` | macOS Apple Silicon |

Each archive ships a single self-contained binary (frontend already embedded —
no Node, no separate `dist/` needed at runtime) plus a `.sha256` checksum file;
the release also carries a combined `SHA256SUMS.txt`. Just download, extract,
and run (`./pglight`, `./pglight --p 8081`, or `./pglight --no-browser`).

Design notes: one Ubuntu runner cross-compiles everything (`CGO_ENABLED=0` —
the backend is pure Go, no cgo), the frontend builds once per job from `npm
ci`, and binaries are `-trimpath -ldflags "-s -w"` stripped. The top-level
`dist/` output dir is git-ignored.

Conventions for contributors live in `AGENTS.md` (backend + frontend rules,
checklists); run `./scripts/check.sh` before finishing any change.

Restart the backend (kills `:8080`, rebuilds, reruns in background):

```sh
./scripts/rerun.sh             # PORT=8080 default
PORT=18080 ./scripts/rerun.sh
```

## Codebase review — 2026-09-20 (auth excluded)

Verification completed against the Docker PostgreSQL 14 fixture:

- `./scripts/check.sh`, `go test -race ./...`, and a live HTTP smoke journey
  (connect → schemas → `SELECT 1` → disconnect) passed.
- `cd web && npm run build` passed. Vite reports a 1.23 MB minified JavaScript
  chunk; code-splitting is a performance follow-up, not a correctness failure.
- Full frontend tests passed under Node 22: 14 Vitest files and 82 tests;
  project-scoped ESLint also passed under the same runtime.
- `cd web && npm test` does not currently run successfully on Node 18.19:
  installed `jsdom@30` requires Node 22+, and Vitest exits with 11
  `ERR_REQUIRE_ESM` setup errors before collecting tests. CI already uses Node 22.

Resolved findings:

- **[P1] Table pagination ignored `has_more`.** `TableWorkspace` now honors the
  backend signal, with regression coverage in
  `web/src/components/TableWorkspace.pagination.test.tsx`.
- **[P1] Table loads had a stale-response race.** Per-tab page and metadata
  request sequences now discard late responses, covered by
  `web/src/hooks/useTableOps.race.test.ts`.
- **[P1] Duplicate result rows shared a selection key.** Grid selection now
  includes the row index when no primary key is available, covered by
  `web/src/hooks/useGridSelection.test.ts`.
- **[P2] Some query-stream failures broke the response contract.** `rows.Values()`
  and `rows.Err()` failures now include `in_txn`, covered by
  `internal/api/query_contract_test.go`.
- **[P2] Selection could survive a query-grid data refresh.** Selection and
  context-cell state now clear when displayed rows change; this is covered by
  the same hook regression test.

Remaining follow-ups:
- **[P2] `/api/table-data` accepts the filter expression verbatim.** This is
  useful for advanced PostgreSQL predicates but is not safe for an untrusted
  caller; keep this endpoint scoped to trusted users or replace the free-form
  predicate with a validated filter AST (`internal/api/data.go`).
- **[P2] Frontend rule drift remains.** Feature components still contain raw
  buttons/input elements and `schemaCache.ts` performs a direct `fetch`, despite
  the shadcn/data-access rules in `AGENTS.md`.

The resolved findings were handled test-first: each regression test was made to
fail against the old behavior, the implementation was then changed, and the
test was rerun successfully. The remaining items need a product/security
decision about trusted callers and a broader frontend component migration.

## Tests (`test/` — canonical, black-box)

All Go tests live in top-level `test/` (package `test`), one file per
domain (`session|query|explorer|data|admin|alter|appdata|db|store|logging`,
`txn_lease` for the `AcquireLease` atomicity + `CloseAll`,
`mockgen|mockgen_full|mockgen_fixes|mockgen_hardening` for the pure generation
engine — no DB needed — and `mockdata|mockdata_full|mockdata_fixes` for the
`/api/mock-data/*` surface; `internal/api/query_contract_test.go` pins the
`{error}+in_txn` contract).
They exercise only exported symbols through the real HTTP-handler surface
(`httptest` + live docker PG, temp sqlite `Store`), never unexported
helpers. White-box checks were ported to endpoint behavior (multi-statement
error locations via `/api/query`, numeric fidelity via `/api/row`+`/api/query`,
FK cross-match via `/api/erd`). Every endpoint has happy + fail + edge cases;
PG-dependent tests skip when the test DB is down, pure unit tests always run.
`db_test.go` covers `Manager.CloseAll` (the shutdown building block);
vitest (jsdom, mocked `fetch`) covers `MockDataDialog` (validation guards,
request-body shapes, preview clamping, failure paths), `SearchSelect` (FK
badge + searchable source), `ConnectionBar` (header wiring), `DataGrid`
(render + row menu), `TabStrip`/`SplitWorkspace`/`useSplit` (split UX),
`useGridSelection` (single/toggle/range keys, refresh reset),
`useTableOps.race` + `TableWorkspace.pagination` (`has_more`, stale-response
token), and `lib/tabs` (slim/restore).

```sh
go test -count=1 ./test/        # full suite
go test -race -count=1 ./test/  # incl. txn concurrency (must stay race-clean)
```

Unit test is the standard: a failing test means the contract broke — fix the
code, not the expectation (expects change only when proven wrong against the
documented contract, e.g. miscounted script lines).

Integration tests (`test/integration_test.go`) boot the full route table
(mirroring `main.go`, behind `logging.Middleware`) with `httptest.NewServer`
and drive cross-endpoint user journeys over real HTTP with `connect` as the
entry point — session lifecycle, txn visibility across `/api/txn` +
`/api/query` + `/api/table-data`, import → cell-edit → bulk-delete flow,
mock-data journey (meta → preview → generate → table-data agreement plus the
`{error}` contract on ghost sessions, bad modes and ghost columns),
explorer chain agreement (schemas/tables/columns/ddl/search/erd), and the
`{error}` contract through transport. Per-endpoint tests prove each handler;
integration proves the handlers share session/txn state through the mux.
`/api/shutdown` is routed in `main.go` but excluded from the integration mux:
it calls `os.Exit`, which would kill the runner.

```sh
go test -count=1 -run TestIntegration ./test/  # HTTP journeys only
```

## Test database (docker/postgres:14-alpine)

```sh
docker compose -f docker/docker-compose.yml up -d   # pg14 + seed demo data
docker compose -f docker/docker-compose.yml down    # stop (keeps data)
docker compose -f docker/docker-compose.yml down -v # reset + reseed
docker exec pglight-pg14 psql -U postgres -d postgres -c "ANALYZE;"  # refresh planner stats after reseed (reltuples starts at -1; TestExplorerBasics reads authors.est_rows)
```

Preconfigured credentials (match the UI defaults, just type the password):
`host=localhost port=5432 user=postgres password=postgres dbname=postgres`.

`docker/init.sql` seeds tables with FK (`authors` → `books` → `reviews`),
a view (`published_books`), matview (`author_stats`), function
(`book_count_by_status`), enum (`mood`), trigger (`trg_books_touch`),
indexes, and a mock-generator fixture (`mock_users`: BIGSERIAL pk, UNIQUE
email, `BETWEEN` + `IN` CHECKs, `created_at <= updated_at`) — enough to
exercise explorer groups, ERD, constraints, stats and
multi-result queries. Verified end-to-end 2026-09-10: connect, schemas,
tables, all 8 object kinds, columns, DDL/view/func defs, constraints,
triggers, table-stats, single + multi-statement query, EXPLAIN, txn
begin→insert→rollback visibility, WHERE-less update refusal, server-info,
stats, locks, activity, roles, extensions, complete, import, ANALYZE.
Safety kept: UPDATE/DELETE without WHERE still refused; maintenance allow-lists ops.
Fix 2026-09-18: `GET /api/ddl` `foreign_keys` now filters `contype='f'` —
previously every constraint (incl. the PK) leaked into that list.

## Docs sync — 2026-09-20

`AGENTS.md` + `PLAN.md` re-scanned against the tree (no code changes):
`internal/api/` is 14 domains + kernel + complete cache (routes ~47 in
`main.go`); `internal/store/` sqlite tables
(`snippets/query_history/aliases/connection_profiles/user_preferences/erd_layouts`);
`internal/mockgen/` pure engine vs `mockdata.go` HTTP; frontend tab kinds
`query|table|browser|erd|docs|object`, hooks
(`useSessions|useTabs|useSplit|useExplorer|useQueryRunner|useTableOps|useObjectOps|useGridSelection`),
full `ui/*` widget list, `dialogs` `promptNullable`/`form` shapes, and the
`schemaCache.ts` direct-`fetch` exception; API list de-duplicated
(`rows-delete`/`row single`, single `query` multi-statement note) and
completed with the missing appdata surface
(`snippets/history/connections/preferences/complete`); status table extended
(mock-data, split view, shutdown, privacy/TLS/numerics); `check.sh` vs CI
gate difference recorded. Remaining follow-ups from the review above still
stand (trusted-caller decision for free-form `filter`, raw-button drift
migration).

## Keyboard command system — 2026-09-20

Implemented the application-wide command registry and rebindable shortcut
resolver in `web/src/commands/` and `web/src/shortcuts/`. Defaults use logical
`Mod` bindings and are merged with versioned server overrides from
`GET|PUT /api/preferences/shortcuts`; the backend validates command IDs,
modifiers, body size, binding count, and serialized length. The Command Palette
now searches commands and database objects together, while Settings supports
recording, conflict replacement, reserved-browser warnings, reset, and v0→v1
shortcut migration. CodeMirror emits command IDs for run/autocomplete so
custom bindings retain selection and completion behavior.
