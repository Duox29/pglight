import { useEffect, useMemo, useState } from 'react'
import { BookOpen } from 'lucide-react'
import { Input } from './ui/input'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { ScrollArea } from './ui/scroll-area'
import { useAppPreference } from '@/lib/storage'
import { cn } from '@/lib/utils'

type DocsLang = 'en' | 'vi'

interface Section {
  id: string
  title: Record<DocsLang, string>
  group: Record<DocsLang, string>
  body: Record<DocsLang, React.ReactNode>
}

function P({ children }: { children: React.ReactNode }) {
  return <p className="text-[13px] leading-relaxed text-muted-foreground">{children}</p>
}

function H({ children }: { children: React.ReactNode }) {
  return <div className="mb-1 mt-2.5 text-[12px] font-semibold text-foreground first:mt-0">{children}</div>
}

function Li({ children }: { children: React.ReactNode }) {
  return <li className="text-[13px] leading-relaxed text-muted-foreground">{children}</li>
}

function K({ children }: { children: React.ReactNode }) {
  return <code className="rounded bg-muted px-1 py-0.5 font-mono text-[12px] text-foreground">{children}</code>
}

function Ul({ children }: { children: React.ReactNode }) {
  return <ul className="mt-1.5 list-disc space-y-1 pl-5">{children}</ul>
}

function Ol({ children }: { children: React.ReactNode }) {
  return <ol className="mt-1.5 list-decimal space-y-1 pl-5">{children}</ol>
}

const GROUP_ORDER: Record<DocsLang, string[]> = {
  en: ['Getting started', 'Browse & search', 'Writing SQL', 'Tables', 'Objects & ERD', 'Right panel', 'System'],
  vi: ['Bắt đầu', 'Duyệt & tìm', 'Viết SQL', 'Bảng', 'Đối tượng & ERD', 'Panel phải', 'Hệ thống'],
}

const UI: Record<DocsLang, { docs: string; filter: string; empty: string; toc: string; langLabel: string }> = {
  en: { docs: 'Documentation', filter: 'Filter sections...', empty: 'No sections match.', toc: 'Docs table of contents', langLabel: 'Documentation language' },
  vi: { docs: 'Tài liệu', filter: 'Lọc mục...', empty: 'Không có mục nào khớp.', toc: 'Mục lục Docs', langLabel: 'Ngôn ngữ tài liệu' },
}

const SECTIONS: Section[] = [
  {
    id: 'connect',
    title: { en: 'Connections and sessions', vi: 'Kết nối và phiên' },
    group: { en: 'Getting started', vi: 'Bắt đầu' },
    body: {
      en: (
        <>
          <P>Open the Connections workspace tab to manage database sessions and saved profiles.</P>
          <H>To connect</H>
          <Ol>
            <Li>Enter <K>host, port, user, password, database</K>, and <K>sslmode</K> (<K>disable</K>, <K>prefer</K>, <K>require</K>, <K>verify-ca</K>, or <K>verify-full</K>).</Li>
            <Li>Select <K>Connect</K>. Active sessions remain available in the Connections tab.</Li>
          </Ol>
          <H>Sessions</H>
          <Ul>
            <Li>Create a credential vault with a master password before saving database passwords. The vault is encrypted in SQLite; the master password cannot be recovered.</Li>
            <Li>The vault auto-locks after 15 minutes without activity. It also locks when the browser session ends or when you select <K>Lock</K>.</Li>
            <Li>When the vault is locked or not configured, pglight never auto-fills or auto-connects with a saved password. Manual password entry still works.</Li>
            <Li><K>Save</K> stores the connection profile and, when the vault is unlocked and a password is entered, encrypts that password in the vault. <K>Delete</K> removes the profile and its secret.</Li>
            <Li><K>Active sessions</K> lists each open session. Select a session to switch to it. Select <K>X</K> to disconnect one session.</Li>
            <Li>A session marked <K>dead</K> lost its server pool, for example after a server restart. Use <K>Reconnect</K> for one session or <K>Reconnect all</K> to restore them with saved credentials.</Li>
            <Li>Select a database in the Explorer to reconnect to that database. Tabs are kept after disconnect and reload on the next connect.</Li>
            <Li>The status bar shows the session count, the active connection, and the <K>Ctrl+K</K> and <K>Ctrl+Enter</K> hints.</Li>
          </Ul>
          <H>Profiles, options, and TLS</H>
          <Ul>
            <Li>Use <K>Advanced</K> to add folders, tags, environment, favorite/default flags, a description, connection timeout, TCP keepalive, application name, search path, Unix socket, and certificate paths.</Li>
            <Li><K>Test</K> performs a temporary PostgreSQL ping without saving the profile. <K>Duplicate</K> copies a saved profile without copying its password into the form.</Li>
            <Li>SSL modes are <K>disable, prefer, require, verify-ca,</K> and <K>verify-full</K>. A non-local host using a mode without certificate verification shows a warning.</Li>
            <Li><K>Auto-connect on startup</K> uses the default profile only when the vault is unlocked. The Power button asks for confirmation, closes sessions, and stops the local server.</Li>
          </Ul>
          <H>Vault recovery</H>
          <Ul>
            <Li>The master password cannot be recovered. If it is forgotten, close pglight and run <K>pglight.exe vault reset</K>.</Li>
            <Li>Reset requires exclusive access to the app store. It removes the vault verifier and saved database passwords, creates a new locked vault, and keeps profiles, folders, tags, and non-secret metadata.</Li>
            <Li>For automation, pipe one new password with <K>--password-stdin --yes</K>. History, snippets, backups, and PostgreSQL server passwords are outside this reset.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Mở tab Workspace → Connections để quản lý session và các profile database.</P>
          <H>Để kết nối</H>
          <Ol>
            <Li>Nhập <K>host, port, user, password, database</K> và <K>sslmode</K> (<K>disable</K>, <K>prefer</K>, <K>require</K>, <K>verify-ca</K> hoặc <K>verify-full</K>).</Li>
            <Li>Chọn <K>Connect</K>. Các session đang hoạt động vẫn hiển thị trong tab Connections.</Li>
          </Ol>
          <H>Phiên</H>
          <Ul>
            <Li>Tạo credential vault bằng master password trước khi lưu password database. Vault được mã hóa trong SQLite và không thể khôi phục master password.</Li>
            <Li>Vault tự khóa sau 15 phút không hoạt động, khi browser session kết thúc hoặc khi chọn <K>Lock</K>.</Li>
            <Li>Khi vault đang khóa hoặc chưa tạo, pglight không tự điền và không tự connect bằng password đã lưu. Vẫn có thể nhập password thủ công.</Li>
            <Li><K>Save</K> lưu profile và mã hóa password vào vault nếu vault đã unlock. <K>Delete</K> xóa profile cùng secret của nó.</Li>
            <Li><K>Active sessions</K> liệt kê từng session đang mở. Chọn một session để chuyển sang. Chọn <K>X</K> để ngắt một session.</Li>
            <Li>Session gắn nhãn <K>dead</K> đã mất pool trên server, ví dụ sau khi server khởi động lại. Dùng <K>Reconnect</K> cho một session hoặc <K>Reconnect all</K> để khôi phục bằng credentials đã lưu.</Li>
            <Li>Chọn một database trong Explorer để kết nối lại sang database đó. Các tab được giữ sau khi ngắt và tải lại ở lần kết nối sau.</Li>
            <Li>Thanh trạng thái hiển thị số session, connection hiện tại và gợi ý <K>Ctrl+K</K>, <K>Ctrl+Enter</K>.</Li>
          </Ul>
          <H>Profile, tùy chọn và TLS</H>
          <Ul>
            <Li>Dùng <K>Advanced</K> để thêm folder, tag, environment, cờ favorite/default, mô tả, timeout kết nối, TCP keepalive, application name, search path, Unix socket và đường dẫn certificate.</Li>
            <Li><K>Test</K> ping PostgreSQL tạm thời mà không lưu profile. <K>Duplicate</K> sao chép profile đã lưu nhưng không đưa password vào form.</Li>
            <Li>SSL mode gồm <K>disable, prefer, require, verify-ca</K> và <K>verify-full</K>. Host không phải local dùng mode không xác minh certificate sẽ hiện cảnh báo.</Li>
            <Li><K>Auto-connect on startup</K> chỉ dùng profile mặc định khi vault đã unlock. Nút Power hỏi xác nhận, đóng các session và dừng server local.</Li>
          </Ul>
          <H>Khôi phục vault</H>
          <Ul>
            <Li>Không thể khôi phục master password. Nếu quên password, đóng pglight rồi chạy <K>pglight.exe vault reset</K>.</Li>
            <Li>Reset cần quyền độc quyền trên app store. Lệnh xóa verifier của vault và password database đã lưu, tạo vault mới ở trạng thái khóa, đồng thời giữ profile, folder, tag và metadata không nhạy cảm.</Li>
            <Li>Chạy tự động bằng cách pipe một password mới với <K>--password-stdin --yes</K>. History, snippets, backup và password trên PostgreSQL không thuộc phạm vi reset này.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'explorer',
    title: { en: 'Explorer', vi: 'Explorer' },
    group: { en: 'Browse & search', vi: 'Duyệt & tìm' },
    body: {
      en: (
        <>
          <P>Use the Explorer to browse databases, schemas, objects, and server objects.</P>
          <H>Structure</H>
          <Ul>
            <Li><K>Databases</K> at the top, then one block per schema with the groups <K>Tables, Views, MatViews, Foreign, Functions, Sequences, Types</K>, then <K>Server objects</K> (<K>Extensions, Roles, ERD</K>).</Li>
            <Li>Select a table, view, matview, or foreign table to open its Data tab. Select a function, sequence, or type to open its Object tab.</Li>
            <Li>Server objects require a live connection and appear dimmed until you connect.</Li>
          </Ul>
          <H>Context menus</H>
          <Ul>
            <Li>Table: <K>Open Data, New Query, Export CSV, Export INSERTs, Copy qualified name, Refresh</K>.</Li>
            <Li>Function, sequence, or type: <K>View definition, New Query, Copy name, Refresh</K>.</Li>
            <Li>Database: <K>New Query, New Schema, Switch to this database, Copy name, Refresh</K>.</Li>
            <Li>Schema: <K>New Query, New Table, Open ERD, Copy name, Refresh</K>.</Li>
          </Ul>
          <H>Filter</H>
          <Ul>
            <Li>The <K>Filter objects</K> box matches <K>schema.name</K> in every group and in databases. It shows the match count and expands groups that contain matches.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Dùng Explorer để duyệt databases, schemas, objects và server objects.</P>
          <H>Cấu trúc</H>
          <Ul>
            <Li><K>Databases</K> ở trên cùng, tiếp theo là từng schema với các nhóm <K>Tables, Views, MatViews, Foreign, Functions, Sequences, Types</K>, cuối cùng là <K>Server objects</K> (<K>Extensions, Roles, ERD</K>).</Li>
            <Li>Chọn table, view, matview hoặc foreign table để mở tab Data. Chọn function, sequence hoặc type để mở tab Object.</Li>
            <Li>Server objects cần connection đang hoạt động và hiển thị mờ cho đến khi kết nối.</Li>
          </Ul>
          <H>Menu chuột phải</H>
          <Ul>
            <Li>Table: <K>Open Data, New Query, Export CSV, Export INSERTs, Copy qualified name, Refresh</K>.</Li>
            <Li>Function, sequence hoặc type: <K>View definition, New Query, Copy name, Refresh</K>.</Li>
            <Li>Database: <K>New Query, New Schema, Switch to this database, Copy name, Refresh</K>.</Li>
            <Li>Schema: <K>New Query, New Table, Open ERD, Copy name, Refresh</K>.</Li>
          </Ul>
          <H>Lọc</H>
          <Ul>
            <Li>Ô <K>Filter objects</K> khớp <K>schema.name</K> trong mọi nhóm và trong databases. Ô này hiển thị số kết quả và mở các nhóm có kết quả.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'search',
    title: { en: 'Global search (Ctrl+K)', vi: 'Tìm kiếm toàn cục (Ctrl+K)' },
    group: { en: 'Browse & search', vi: 'Duyệt & tìm' },
    body: {
      en: (
        <>
          <P>Use global search to find a table or column and open it.</P>
          <H>To search</H>
          <Ol>
            <Li>Open search with the <K>Search objects</K> button or <K>Ctrl/Command + K</K>.</Li>
            <Li>Type at least 2 characters. Requests are debounced by 200 ms.</Li>
            <Li>Press <K>Enter</K> to open the top hit, or select a row. Press <K>Esc</K> to close.</Li>
          </Ol>
          <H>Scope</H>
          <Ul>
            <Li>Search covers table names, schema names, and column names. Selecting a column opens the table that contains it.</Li>
            <Li>System schemas (<K>pg_catalog</K>, <K>information_schema</K>, <K>pg_</K> schemas) are excluded. Results are capped per kind.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Dùng tìm kiếm toàn cục để tìm table hoặc cột rồi mở ra.</P>
          <H>Để tìm kiếm</H>
          <Ol>
            <Li>Mở tìm kiếm bằng nút <K>Search objects</K> hoặc <K>Ctrl/Command + K</K>.</Li>
            <Li>Nhập ít nhất 2 ký tự. Yêu cầu được debounce 200 ms.</Li>
            <Li>Nhấn <K>Enter</K> để mở kết quả đầu, hoặc chọn một dòng. Nhấn <K>Esc</K> để đóng.</Li>
          </Ol>
          <H>Phạm vi</H>
          <Ul>
            <Li>Tìm kiếm bao gồm tên table, tên schema và tên cột. Chọn một cột sẽ mở bảng chứa cột đó.</Li>
            <Li>Schema hệ thống (<K>pg_catalog</K>, <K>information_schema</K>, schema <K>pg_</K>) được loại trừ. Kết quả bị giới hạn theo từng loại.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'tabs',
    title: { en: 'Tabs and layout', vi: 'Tabs và layout' },
    group: { en: 'Browse & search', vi: 'Duyệt & tìm' },
    body: {
      en: (
        <>
          <P>The workspace has a resizable explorer column and a main tab area. Tab kinds include <K>Query, Table, Object, Browser, ERD, Docs,</K> and <K>Workspace</K>. Workspace contains History, database monitoring, settings, logs, and Quick Access in one navigation bar.</P>
          <Ul>
            <Li>The dot marks tab state: blue for the active tab, amber for an edited query that has not run. The DB badge shows the tab session database.</Li>
            <Li>To close a tab, use the <K>X</K> button or middle-click. Right-click a tab for <K>Close, Close Others, Close to the Right, Close to the Left,</K> and <K>Close All</K>.</Li>
            <Li>Use the <K>+ Query</K> button at the end of the tab bar to open a new console.</Li>
            <Li>Right-click a tab and choose <K>Split Right</K> or <K>Split Down</K> to show two panes. The split is limited to two panes; use the secondary picker to switch tabs, or use swap/close controls. Pane direction and layout sizes persist.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Workspace gồm cột explorer co giãn và vùng tab chính. Các loại tab gồm <K>Query, Table, Object, Browser, ERD, Docs</K> và <K>Workspace</K>. Workspace chứa History, giám sát database, settings, logs và Quick Access trong một thanh điều hướng.</P>
          <Ul>
            <Li>Chấm tròn báo trạng thái tab: xanh cho tab đang mở, hổ phách cho query đã sửa nhưng chưa chạy. Badge DB cho biết database của session gắn với tab.</Li>
            <Li>Để đóng tab, dùng nút <K>X</K> hoặc middle-click. Chuột phải lên tab để dùng <K>Close, Close Others, Close to the Right, Close to the Left</K> và <K>Close All</K>.</Li>
            <Li>Dùng nút <K>+ Query</K> ở cuối thanh tab để mở console mới.</Li>
            <Li>Chuột phải lên tab rồi chọn <K>Split Right</K> hoặc <K>Split Down</K> để mở hai pane. Split tối đa hai pane; dùng picker phụ để đổi tab hoặc nút swap/close. Hướng pane và kích thước layout được lưu lại.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'query',
    title: { en: 'Query console', vi: 'Query console' },
    group: { en: 'Writing SQL', vi: 'Viết SQL' },
    body: {
      en: (
        <>
          <P>Each Query tab is an independent console with its own session.</P>
          <H>To run a query</H>
          <Ol>
            <Li>Type SQL in the editor, or select part of the script to run only that part.</Li>
            <Li>Press <K>Ctrl+Enter</K> or select <K>Run</K>. A single <K>SELECT</K> is wrapped with a limiter based on the limit picker.</Li>
          </Ol>
          <H>Editor and completion</H>
          <Ul>
            <Li>The editor is a SQL editor with line wrap and no gutter. <K>Ctrl+Space</K> opens completion manually. <K>Tab</K> or <K>Enter</K> accepts a suggestion.</Li>
            <Li>Completion is context aware: after <K>FROM</K> it suggests tables; after <K>alias.</K> it suggests columns of that table. The schema snapshot is cached for 60 seconds per session, refreshed after DDL, and can be refreshed manually.</Li>
            <Li><K>Format</K> uppercases keywords and breaks clauses. <K>Snippet</K> saves the current SQL into the Snippets panel.</Li>
          </Ul>
          <H>Toolbar</H>
          <Ul>
            <Li><K>Run</K> starts execution. The square button cancels the running query of that tab. With several concurrent runs, use the Workspace Activity view to cancel a specific backend.</Li>
            <Li>The transaction cluster of that tab session, plus the <K>open transaction</K> or <K>no transaction</K> badge. See Transactions.</Li>
            <Li><K>Explain</K> runs <K>EXPLAIN ANALYZE</K> with buffers. It explains the selected SQL, or the whole script when nothing is selected, and the text plan includes timing data.</Li>
            <Li>The limit picker offers <K>200 rows, 1000 rows,</K> and <K>no limit</K>. The <K>Export</K> menu of the first result offers <K>CSV, JSON,</K> and <K>INSERTs</K>.</Li>
            <Li><K>Clear</K> removes results, plans, and errors. The meta line shows row count and duration.</Li>
          </Ul>
          <H>Results</H>
          <Ul>
            <Li>A script with several statements runs in order with one card per statement, newest first. Each card shows the statement preview, row count, and duration. A failing statement reports its position. Statements without a table show <K>OK</K> with the affected row count.</Li>
            <Li>Select a column header to sort with Postgres semantics: numbers numerically, <K>false</K> before <K>true</K>, NULLs last. Select a cell to copy its value.</Li>
            <Li>A tab restored from the previous session shows its last result as a labeled snapshot. Select <K>Run</K> to refresh it.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Mỗi tab Query là một console độc lập với session riêng.</P>
          <H>Để chạy query</H>
          <Ol>
            <Li>Nhập SQL trong editor, hoặc bôi đen một phần script để chỉ chạy phần đó.</Li>
            <Li>Nhấn <K>Ctrl+Enter</K> hoặc chọn <K>Run</K>. Một câu <K>SELECT</K> đơn được bọc limiter theo mức limit đang chọn.</Li>
          </Ol>
          <H>Editor và gợi ý</H>
          <Ul>
            <Li>Editor SQL tự xuống dòng, không có gutter. <K>Ctrl+Space</K> mở gợi ý thủ công. <K>Tab</K> hoặc <K>Enter</K> nhận gợi ý.</Li>
            <Li>Gợi ý theo ngữ cảnh: sau <K>FROM</K> gợi ý bảng; sau <K>alias.</K> gợi ý cột của bảng đó. Snapshot schema được cache 60 giây theo session, làm mới sau DDL và có thể làm mới thủ công.</Li>
            <Li><K>Format</K> viết hoa keywords và ngắt mệnh đề. <K>Snippet</K> lưu SQL hiện tại vào panel Snippets.</Li>
          </Ul>
          <H>Toolbar</H>
          <Ul>
            <Li><K>Run</K> bắt đầu chạy. Nút vuông hủy query đang chạy của tab đó. Với nhiều query chạy đồng thời, dùng view Activity trong Workspace để hủy một backend cụ thể.</Li>
            <Li>Cụm transaction của session gắn với tab, kèm badge <K>open transaction</K> hoặc <K>no transaction</K>. Xem mục Transactions.</Li>
            <Li><K>Explain</K> chạy <K>EXPLAIN ANALYZE</K> kèm buffers. Nó explain phần SQL đang bôi đen, hoặc toàn bộ script nếu không chọn gì; text plan gồm timing.</Li>
            <Li>Ô limit gồm <K>200 rows, 1000 rows</K> và <K>no limit</K>. Menu <K>Export</K> của result đầu gồm <K>CSV, JSON</K> và <K>INSERTs</K>.</Li>
            <Li><K>Clear</K> xóa results, plan và lỗi. Dòng meta hiển thị số dòng và thời gian chạy.</Li>
          </Ul>
          <H>Kết quả</H>
          <Ul>
            <Li>Script nhiều câu lệnh chạy tuần tự, mỗi câu một card, card mới nhất trước. Mỗi card hiển thị preview câu lệnh, số dòng và thời gian. Câu lệnh lỗi báo rõ vị trí. Câu lệnh không trả bảng hiển thị <K>OK</K> kèm số dòng bị ảnh hưởng.</Li>
            <Li>Chọn header cột để sắp xếp theo ngữ nghĩa Postgres: số so theo số, <K>false</K> trước <K>true</K>, NULL cuối. Chọn một cell để copy giá trị.</Li>
            <Li>Tab khôi phục từ phiên trước hiển thị result cuối dưới dạng snapshot có nhãn. Chọn <K>Run</K> để làm mới.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'txn',
    title: { en: 'Transactions', vi: 'Transactions' },
    group: { en: 'Writing SQL', vi: 'Viết SQL' },
    body: {
      en: (
        <>
          <P>Transaction controls are inside each Query tab toolbar, next to the <K>user@host/dbname</K> label. There is no global transaction bar, so each control applies to its own tab session.</P>
          <Ul>
            <Li>The <K>autocommit</K> switch. The <K>Begin, Commit,</K> and <K>Rollback</K> buttons. <K>Begin</K> is disabled inside a transaction; <K>Commit</K> and <K>Rollback</K> are disabled outside one.</Li>
            <Li>The badge shows <K>open transaction</K> in amber or <K>no transaction</K> in green.</Li>
            <Li>With autocommit off, running DML opens a transaction. Query, table, row, import, and alter paths observe the open transaction, so uncommitted changes are visible inside it. Responses include the <K>in_txn</K> flag.</Li>
            <Li>The <K>VACUUM</K> family is refused inside an open transaction because Postgres does not permit VACUUM in a transaction block.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Điều khiển transaction nằm trong toolbar của từng tab Query, cạnh nhãn <K>user@host/dbname</K>. Không có thanh transaction chung, mỗi cụm chỉ áp dụng cho session của tab đó.</P>
          <Ul>
            <Li>Switch <K>autocommit</K>. Các nút <K>Begin, Commit</K> và <K>Rollback</K>. <K>Begin</K> disable trong transaction; <K>Commit</K> và <K>Rollback</K> disable ngoài transaction.</Li>
            <Li>Badge hiển thị <K>open transaction</K> màu hổ phách hoặc <K>no transaction</K> màu xanh lá.</Li>
            <Li>Khi tắt autocommit, chạy DML sẽ mở transaction. Các đường query, table, row, import và alter đều thấy transaction đang mở, nên thay đổi chưa commit hiển thị được trong đó. Response gồm cờ <K>in_txn</K>.</Li>
            <Li>Họ <K>VACUUM</K> bị từ chối trong transaction đang mở vì Postgres không cho VACUUM trong transaction block.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'table-data',
    title: { en: 'Tables: data', vi: 'Bảng: dữ liệu' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>The header shows the table name with <K>schema, total rows,</K> and <K>owner</K>. Use the <K>WHERE</K> box (for example <K>id &gt; 10</K>) and the <K>ORDER</K> box (for example <K>id DESC</K>), then <K>Apply</K>. Page with <K>Previous</K> and <K>Next</K>. The meta line shows row count, total, duration, and the edit hint.</P>
          <H>To edit, insert, or delete a row</H>
          <Ol>
            <Li>Double-click a cell to edit it. Use the <K>Set NULL</K> button for NULL. Editing requires a primary key. A NULL primary key value blocks the edit. Single edits and deletes are PK-scoped and the server refuses them unless exactly one row matches. The server refuses UPDATE and DELETE without a key.</Li>
            <Li>Single-click selects. Right-click a cell copies its value.</Li>
            <Li>Use the row action buttons to copy the row as INSERT or to delete the row with confirmation.</Li>
            <Li>Use the <K>Row</K> button to open the insert form. Empty means skip the column. The per-field <K>N</K> toggle means NULL.</Li>
          </Ol>
          <H>Bulk selection</H>
          <Ul>
            <Li><K>Ctrl/Command-click</K> toggles rows. <K>Shift-click</K> selects a range. Selection is keyed by primary key values, or by serialized row content when the table has no primary key.</Li>
            <Li>Right-click the grid for the selection menu with the selected count: <K>Export</K> to <K>CSV, JSON,</K> or <K>INSERTs</K>; <K>Copy</K> as CSV; <K>Delete</K> with confirmation.</Li>
          </Ul>
          <H>To import CSV</H>
          <Ol>
            <Li>Select <K>Import CSV</K> and pick a <K>.csv, .tsv,</K> or <K>.txt</K> file. <K>.tsv</K> uses tab delimiters.</Li>
            <Li>The header maps by column name, case insensitive, with positional fallback. Files with uneven row widths are rejected with the reported widths.</Li>
            <Li>Confirm the dialog showing row count, target table, and columns. Server import is atomic, capped at 20,000 rows in batches of 500.</Li>
          </Ol>
        </>
      ),
      vi: (
        <>
          <P>Header hiển thị tên bảng kèm <K>schema, tổng rows</K> và <K>owner</K>. Dùng ô <K>WHERE</K> (ví dụ <K>id &gt; 10</K>) và ô <K>ORDER</K> (ví dụ <K>id DESC</K>), rồi chọn <K>Apply</K>. Phân trang bằng <K>Previous</K> và <K>Next</K>. Dòng meta hiển thị số dòng, tổng, thời gian và gợi ý sửa.</P>
          <H>Để sửa, thêm hoặc xóa dòng</H>
          <Ol>
            <Li>Double-click một cell để sửa. Dùng nút <K>Set NULL</K> cho NULL. Sửa cần primary key. Giá trị primary key NULL sẽ chặn sửa. Sửa/xóa một dòng theo khóa chính và server từ chối trừ khi khớp đúng một dòng. Server từ chối UPDATE và DELETE không khóa.</Li>
            <Li>Single-click để chọn. Chuột phải lên cell để copy giá trị.</Li>
            <Li>Dùng nút actions trên dòng để copy dòng dạng INSERT hoặc xóa dòng kèm xác nhận.</Li>
            <Li>Dùng nút <K>Row</K> để mở form insert. Ô trống nghĩa là bỏ qua cột. Nút <K>N</K> từng ô nghĩa là NULL.</Li>
          </Ol>
          <H>Chọn nhiều dòng</H>
          <Ul>
            <Li><K>Ctrl/Command-click</K> bật tắt từng dòng. <K>Shift-click</K> chọn một đoạn. Khóa chọn theo giá trị primary key, hoặc theo nội dung dòng serialize khi bảng không có primary key.</Li>
            <Li>Chuột phải lên lưới để mở menu theo số dòng đã chọn: <K>Export</K> sang <K>CSV, JSON</K> hoặc <K>INSERTs</K>; <K>Copy</K> dạng CSV; <K>Delete</K> kèm xác nhận.</Li>
          </Ul>
          <H>Để import CSV</H>
          <Ol>
            <Li>Chọn <K>Import CSV</K> rồi chọn file <K>.csv, .tsv</K> hoặc <K>.txt</K>. File <K>.tsv</K> dùng delimiter tab.</Li>
            <Li>Header map theo tên cột, không phân biệt hoa thường, dự phòng theo vị trí. File lệch số cột bị từ chối kèm số cột từng dòng.</Li>
            <Li>Xác nhận hộp thoại ghi số dòng, bảng đích và cột. Import trên server mang tính nguyên tử, tối đa 20.000 dòng theo batch 500.</Li>
          </Ol>
        </>
      ),
    },
  },
  {
    id: 'mock-data',
    title: { en: 'Generate mock data', vi: 'Tạo mock data' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>Open <K>Generate</K> from a table Data tab to create test rows without leaving the workspace.</P>
          <H>Simple mode</H>
          <Ul>
            <Li>Simple uses datatype-only generators. Identity, generated, serial, and default-backed columns are left for PostgreSQL to fill.</Li>
            <Li>PostgreSQL remains the final validator for CHECK, UNIQUE, foreign-key, and other constraints. Use Advanced when those rules need to be planned before insertion.</Li>
          </Ul>
          <H>Advanced mode</H>
          <Ul>
            <Li>Advanced loads normalized column, enum, foreign-key, CHECK, primary-key, and unique metadata. Configure Auto, DB Default, NULL, Constant, numeric/date ranges, Choice, Sequence, JSON/Array, semantic values, foreign keys, and relative datetimes per column.</Li>
            <Li>Set nullable probability, single-column uniqueness, and structured compare constraints. Composite foreign keys are generated as one tuple so members cannot be mixed.</Li>
            <Li>Enter an optional integer seed for repeatable output. <K>Preview</K> shows a small sample and warnings; <K>Generate</K> inserts up to 20,000 rows atomically and reports whether the session transaction remains open.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Mở <K>Generate</K> từ tab Data của bảng để tạo các dòng test ngay trong workspace.</P>
          <H>Chế độ Simple</H>
          <Ul>
            <Li>Simple chỉ dùng generator theo datatype. Cột identity, generated, serial và có default được để PostgreSQL tự điền.</Li>
            <Li>PostgreSQL vẫn là validator cuối cho CHECK, UNIQUE, foreign key và các constraint khác. Dùng Advanced khi cần lập kế hoạch theo các luật này trước khi insert.</Li>
          </Ul>
          <H>Chế độ Advanced</H>
          <Ul>
            <Li>Advanced tải metadata chuẩn hóa của column, enum, foreign key, CHECK, primary key và unique. Có thể cấu hình Auto, DB Default, NULL, Constant, khoảng số/ngày, Choice, Sequence, JSON/Array, giá trị ngữ nghĩa, foreign key và datetime tương đối theo từng cột.</Li>
            <Li>Thiết lập xác suất NULL, unique một cột và compare constraint dạng cấu trúc. Composite foreign key được sinh như một tuple nên các member không bị trộn sai.</Li>
            <Li>Nhập seed nguyên để lặp lại kết quả. <K>Preview</K> hiển thị mẫu nhỏ và warning; <K>Generate</K> insert tối đa 20.000 dòng nguyên tử và báo session còn transaction mở hay không.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'table-structure',
    title: { en: 'Tables: structure', vi: 'Bảng: cấu trúc' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>The table header has five tabs: <K>Data, Structure, SQL, Indexes,</K> and <K>Stats</K>. Structure switches between <K>Columns, Constraints,</K> and <K>Triggers</K>. The Columns tab lists <K>name, type, nullable, default, pk,</K> and <K>comment</K>. Double-click a row to edit it.</P>
          <Ul>
            <Li><K>Rename table</K> renames the table through a prompt. <K>+ Column</K> opens the Add dialog.</Li>
            <Li><K>Name</K> accepts lowercase letters, digits, and underscores. Renames use ALTER RENAME.</Li>
            <Li><K>Type</K> suggests grouped types (Numeric, Text, Logical, Date/time, JSON, IDs, Arrays). Custom enum and domain types are accepted. Type changes rewrite with <K>USING column::new_type</K>.</Li>
            <Li><K>Nullable</K> off issues SET NOT NULL. Existing NULL values block the statement. <K>Default</K> is a SQL expression such as <K>now(), 0,</K> or <K>'foo'</K>. Empty removes the default.</Li>
            <Li>The trash button drops the column with a danger confirmation.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Header của bảng gồm năm tab: <K>Data, Structure, SQL, Indexes</K> và <K>Stats</K>. Structure chuyển giữa <K>Columns, Constraints</K> và <K>Triggers</K>. Tab Columns liệt kê <K>name, type, nullable, default, pk</K> và <K>comment</K>. Double-click một dòng để sửa.</P>
          <Ul>
            <Li><K>Rename table</K> đổi tên bảng qua hộp thoại. <K>+ Column</K> mở dialog thêm cột.</Li>
            <Li><K>Name</K> chấp nhận chữ thường, số và gạch dưới. Đổi tên dùng ALTER RENAME.</Li>
            <Li><K>Type</K> gợi ý theo nhóm (Numeric, Text, Logical, Date/time, JSON, IDs, Arrays). Enum và domain tự định nghĩa được chấp nhận. Đổi kiểu rewrite bằng <K>USING column::new_type</K>.</Li>
            <Li><K>Nullable</K> tắt sẽ chèn SET NOT NULL. NULL hiện có sẽ chặn lệnh. <K>Default</K> là biểu thức SQL như <K>now(), 0</K> hoặc <K>'foo'</K>. Để trống là gỡ default.</Li>
            <Li>Nút xóa drop cột kèm xác nhận danger.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'table-indexes',
    title: { en: 'Tables: indexes', vi: 'Bảng: indexes' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>The Indexes tab lists <K>name</K> and <K>definition</K>. Each row offers Rename and Drop. <K>+ Index</K> opens the Create dialog.</P>
          <H>Create index dialog</H>
          <Ul>
            <Li><K>Name</K> is optional. Empty lets Postgres name the index. The <K>Unique</K> switch controls uniqueness.</Li>
            <Li><K>Method</K>: <K>btree</K> for equality and range (default), <K>hash</K> for equality only, <K>gin</K> for jsonb, arrays, and full text search, <K>gist</K> for geometry and ranges, <K>spgist</K> for partitioned search, <K>brin</K> for large append-only tables.</Li>
            <Li><K>Keys</K>: select columns in order, each with <K>ASC</K> or <K>DESC</K>. Add expression keys such as <K>(lower(email))</K> through the input with <K>Enter</K>. A live <K>CREATE INDEX</K> preview is shown.</Li>
            <Li><K>Include</K> lists covering columns without heap access, btree only. <K>Where</K> defines a partial index predicate such as <K>active IS TRUE</K>. Empty means the whole table. The index name is unqualified and lands in the table schema.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab Indexes liệt kê <K>name</K> và <K>definition</K>. Mỗi dòng có Rename và Drop. <K>+ Index</K> mở dialog tạo index.</P>
          <H>Dialog Create index</H>
          <Ul>
            <Li><K>Name</K> tùy chọn. Để trống cho Postgres tự đặt tên. Switch <K>Unique</K> điều khiển tính duy nhất.</Li>
            <Li><K>Method</K>: <K>btree</K> cho bằng và khoảng (mặc định), <K>hash</K> chỉ cho bằng, <K>gin</K> cho jsonb, array và full text search, <K>gist</K> cho geometry và range, <K>spgist</K> cho tìm kiếm phân vùng, <K>brin</K> cho bảng append-only lớn.</Li>
            <Li><K>Keys</K>: chọn cột theo thứ tự, mỗi key kèm <K>ASC</K> hoặc <K>DESC</K>. Thêm key biểu thức như <K>(lower(email))</K> qua ô nhập với <K>Enter</K>. Preview <K>CREATE INDEX</K> hiển thị trực tiếp.</Li>
            <Li><K>Include</K> liệt kê cột covering đọc không qua heap, chỉ btree. <K>Where</K> định nghĩa predicate cho partial index như <K>active IS TRUE</K>. Để trống nghĩa là cả bảng. Tên index không kèm schema và nằm trong schema của bảng.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'table-constraints',
    title: { en: 'Tables: constraints', vi: 'Bảng: constraints' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>The Constraints tab lists <K>name, type,</K> and <K>definition</K>. Each row offers Drop. <K>+ Constraint</K> opens the Add dialog.</P>
          <Ul>
            <Li><K>Name</K> is optional. Empty lets Postgres name the constraint.</Li>
            <Li><K>Definition</K> starts with <K>CHECK, UNIQUE, PRIMARY KEY, FOREIGN KEY,</K> or <K>EXCLUDE</K>. Examples: <K>CHECK (price &gt; 0)</K>, <K>UNIQUE (email)</K>, <K>FOREIGN KEY (author_id) REFERENCES authors(id)</K>.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab Constraints liệt kê <K>name, type</K> và <K>definition</K>. Mỗi dòng có Drop. <K>+ Constraint</K> mở dialog thêm.</P>
          <Ul>
            <Li><K>Name</K> tùy chọn. Để trống cho Postgres tự đặt tên.</Li>
            <Li><K>Definition</K> bắt đầu bằng <K>CHECK, UNIQUE, PRIMARY KEY, FOREIGN KEY</K> hoặc <K>EXCLUDE</K>. Ví dụ: <K>CHECK (price &gt; 0)</K>, <K>UNIQUE (email)</K>, <K>FOREIGN KEY (author_id) REFERENCES authors(id)</K>.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'table-triggers',
    title: { en: 'Tables: triggers', vi: 'Bảng: triggers' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>The Triggers tab lists <K>name, table, event, timing,</K> and <K>statement</K>, plus the <K>enabled</K> or <K>disabled</K> badge. Each row has an enable switch and Drop. <K>+ Trigger</K> opens the Create dialog.</P>
          <H>Create trigger dialog</H>
          <Ul>
            <Li><K>Timing</K>: <K>BEFORE</K> can modify NEW or skip the write; <K>AFTER</K> sees the final row; <K>INSTEAD OF</K> is for views.</Li>
            <Li><K>Events</K> accepts several values: <K>INSERT, UPDATE, DELETE, TRUNCATE</K>. <K>For each</K> is <K>ROW</K> (supports NEW and OLD) or <K>STATEMENT</K> (once per statement).</Li>
            <Li><K>Function</K> is a trigger function such as <K>audit_fn()</K> and has to exist. <K>Update of</K> appears for UPDATE and limits firing columns. Empty means any UPDATE. <K>When</K> is a row condition with OLD and NEW, for example <K>OLD.status IS DISTINCT FROM NEW.status</K>. Empty means it fires.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab Triggers liệt kê <K>name, table, event, timing</K> và <K>statement</K>, kèm badge <K>enabled</K> hoặc <K>disabled</K>. Mỗi dòng có switch bật tắt và Drop. <K>+ Trigger</K> mở dialog tạo trigger.</P>
          <H>Dialog Create trigger</H>
          <Ul>
            <Li><K>Timing</K>: <K>BEFORE</K> sửa NEW hoặc bỏ qua write; <K>AFTER</K> thấy dòng cuối; <K>INSTEAD OF</K> dùng cho view.</Li>
            <Li><K>Events</K> nhận nhiều giá trị: <K>INSERT, UPDATE, DELETE, TRUNCATE</K>. <K>For each</K> là <K>ROW</K> (dùng NEW và OLD) hoặc <K>STATEMENT</K> (một lần mỗi câu lệnh).</Li>
            <Li><K>Function</K> là hàm trigger như <K>audit_fn()</K> và phải tồn tại. <K>Update of</K> xuất hiện với UPDATE và giới hạn cột kích trigger. Để trống nghĩa là mọi UPDATE. <K>When</K> là điều kiện dòng dùng OLD và NEW, ví dụ <K>OLD.status IS DISTINCT FROM NEW.status</K>. Để trống nghĩa là trigger chạy.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'table-sql',
    title: { en: 'Tables: SQL and DDL', vi: 'Bảng: SQL và DDL' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>The SQL tab shows the server definition with a reconstructed fallback when the server form is missing.</P>
          <Ul>
            <Li>Owner and comment line, then the <K>CREATE TABLE schema.table</K> block with a <K>Copy</K> button.</Li>
            <Li>Separate Constraints and Indexes blocks in scrollable mono text. Empty blocks show a dash.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab SQL hiển thị định nghĩa từ server, kèm bản dựng lại khi thiếu bản server.</P>
          <Ul>
            <Li>Dòng owner và comment, tiếp theo là khối <K>CREATE TABLE schema.table</K> kèm nút <K>Copy</K>.</Li>
            <Li>Khối Constraints và Indexes riêng dạng text mono cuộn được. Khối trống hiển thị dấu gạch ngang.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'table-stats',
    title: { en: 'Tables: stats and maintenance', vi: 'Bảng: stats và maintenance' },
    group: { en: 'Tables', vi: 'Bảng' },
    body: {
      en: (
        <>
          <P>The Stats tab shows a key and value grid: sizes, sequential and index scans, live and dead tuples, and vacuum and analyze ages.</P>
          <Ul>
            <Li><K>VACUUM, ANALYZE,</K> and <K>REINDEX</K> buttons, each with confirmation. Stats reload after the operation.</Li>
            <Li>Permitted operations are <K>vacuum, vacuum_full, analyze,</K> and <K>reindex</K>. They are refused inside an open transaction.</Li>
            <Li>The <K>ERD</K> button opens the diagram of the schema.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab Stats hiển thị lưới key và value: kích thước, seq và index scans, live và dead tuples, tuổi vacuum và analyze.</P>
          <Ul>
            <Li>Các nút <K>VACUUM, ANALYZE</K> và <K>REINDEX</K>, mỗi nút kèm xác nhận. Stats tải lại sau thao tác.</Li>
            <Li>Các thao tác cho phép gồm <K>vacuum, vacuum_full, analyze</K> và <K>reindex</K>. Các thao tác này bị từ chối trong transaction đang mở.</Li>
            <Li>Nút <K>ERD</K> mở sơ đồ của schema.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'objects',
    title: { en: 'Functions, sequences, and types', vi: 'Functions, sequences và types' },
    group: { en: 'Objects & ERD', vi: 'Đối tượng & ERD' },
    body: {
      en: (
        <>
          <P>Object tabs document one function, sequence, or type. The header shows the name with a <K>kind and schema</K> badge.</P>
          <Ul>
            <Li><K>Edit</K> applies to sequences and functions. <K>Rename</K> applies to sequences and types. Change a function with <K>CREATE OR REPLACE</K>.</Li>
            <Li><K>Drop</K> asks for confirmation. Dropping a function resolves every overload through <K>regprocedure</K> and confirms the count.</Li>
            <Li><K>Open in query</K> sends the definition to a new console. <K>Reload</K> reloads the definition.</Li>
            <Li>The <K>Properties</K> card lists metadata. The <K>Definition</K> card has a <K>Copy</K> button.</Li>
            <Li>Sequence editing covers increment, min, max, cache, restart, and cycle through <K>ALTER SEQUENCE</K>. Empty numeric fields keep the current value. Non-integer input is rejected.</Li>
            <Li>Function editing uses a text area for the full definition. Empty input is rejected. Enum types offer an <K>Add value</K> box through <K>ALTER TYPE ... ADD VALUE</K>.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab Object mô tả một function, sequence hoặc type. Header hiển thị tên kèm badge <K>kind và schema</K>.</P>
          <Ul>
            <Li><K>Edit</K> áp dụng cho sequence và function. <K>Rename</K> áp dụng cho sequence và type. Đổi function bằng <K>CREATE OR REPLACE</K>.</Li>
            <Li><K>Drop</K> yêu cầu xác nhận. Xóa function resolve mọi overload qua <K>regprocedure</K> rồi xác nhận số lượng.</Li>
            <Li><K>Open in query</K> đưa definition sang console mới. <K>Reload</K> tải lại định nghĩa.</Li>
            <Li>Card <K>Properties</K> liệt kê metadata. Card <K>Definition</K> có nút <K>Copy</K>.</Li>
            <Li>Sửa sequence gồm increment, min, max, cache, restart và cycle qua <K>ALTER SEQUENCE</K>. Ô số để trống giữ giá trị hiện tại. Input không phải số nguyên bị từ chối.</Li>
            <Li>Sửa function dùng text area cho toàn bộ definition. Input trống bị từ chối. Kiểu enum có ô <K>Add value</K> qua <K>ALTER TYPE ... ADD VALUE</K>.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'browser',
    title: { en: 'Extensions and roles', vi: 'Extensions và roles' },
    group: { en: 'Objects & ERD', vi: 'Đối tượng & ERD' },
    body: {
      en: (
        <>
          <P>Browser tabs open from Server objects in the Explorer. Each tab is a grid with a <K>Reload</K> button.</P>
          <Ul>
            <Li>Extensions shows <K>name, default_version, installed_version,</K> and <K>comment</K> for installed and available extensions.</Li>
            <Li>Roles shows <K>name, superuser, login, createdb,</K> and <K>member_of</K> for users and groups.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab Browser mở từ Server objects trong Explorer. Mỗi tab là một lưới kèm nút <K>Reload</K>.</P>
          <Ul>
            <Li>Extensions hiển thị <K>name, default_version, installed_version</K> và <K>comment</K> cho extension đã cài và có sẵn.</Li>
            <Li>Roles hiển thị <K>name, superuser, login, createdb</K> và <K>member_of</K> cho users và groups.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'erd',
    title: { en: 'ERD diagram', vi: 'Sơ đồ ERD' },
    group: { en: 'Objects & ERD', vi: 'Đối tượng & ERD' },
    body: {
      en: (
        <>
          <P>The ERD tab shows the foreign key graph of one schema on a dark canvas.</P>
          <H>Toolbar</H>
          <Ul>
            <Li>Schema picker, <K>Reload, Fit view, Auto arrange, Reset layout</K>, table filter, and the <K>N tables and M relations</K> count. Auto-layout packs related tables by relation layers and unrelated tables into a balanced grid, then flow-packs the blocks so the canvas stays roughly square.</Li>
            <Li>The line switch applies to every edge at once: <K>Curved (bezier), Straight,</K> or <K>Orthogonal (smoothstep)</K>.</Li>
          </Ul>
          <H>Reading the diagram</H>
          <Ul>
            <Li>A node is a table: name, relation count, and schema in the header; up to 30 columns, then a <K>+N more</K> footer. Primary key columns show an amber key icon. Foreign key columns show a sky link icon. Types are truncated.</Li>
            <Li>Arrows run from the child column to the parent table. A chip labels the column when several foreign keys link the same table pair. The selected edge shows a blue dot.</Li>
            <Li>A schema without foreign keys shows a dashed banner with tables and no edges.</Li>
          </Ul>
          <H>Interaction</H>
          <Ul>
            <Li>Select a node to open the table. Right-click a node for <K>Open table, Copy qualified name,</K> and <K>Focus related tables</K>.</Li>
            <Li>Drag nodes to arrange them. Positions persist per session and schema. <K>Auto arrange</K> rebuilds the balanced layout and saves it; <K>Reset layout</K> clears saved positions, rebuilds the automatic layout, and fits the view.</Li>
            <Li>Filter dims non-matching tables. <K>Enter</K> focuses the first match. Selecting a node or edge dims unrelated items and shows a detail bar.</Li>
            <Li>Scroll or pinch to zoom from 0.1 to 1.75. The minimap is round. Zoom and fit controls are at the bottom left.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tab ERD hiển thị đồ thị khóa ngoại của một schema trên canvas tối.</P>
          <H>Toolbar</H>
          <Ul>
            <Li>Chọn schema, <K>Reload, Fit view, Auto arrange, Reset layout</K>, lọc bảng và số đếm <K>N tables và M relations</K>. Auto-layout gom bảng liên quan theo tầng quan hệ, bảng rời rạc vào lưới cân bằng rồi xếp khối vừa khung.</Li>
            <Li>Cụm chuyển kiểu đường áp cho mọi cạnh cùng lúc: <K>Curved (bezier), Straight</K> hoặc <K>Orthogonal (smoothstep)</K>.</Li>
          </Ul>
          <H>Đọc sơ đồ</H>
          <Ul>
            <Li>Một node là một bảng: tên, số relations và schema ở header; tối đa 30 cột, tiếp theo là dòng <K>+N more</K>. Cột primary key có icon chìa khóa hổ phách. Cột foreign key có icon link xanh da trời. Kiểu dữ liệu được rút gọn.</Li>
            <Li>Mũi tên đi từ cột con tới bảng cha. Chip ghi nhãn cột khi nhiều khóa ngoại nối cùng một cặp bảng. Cạnh được chọn có chấm xanh.</Li>
            <Li>Schema không có khóa ngoại hiển thị banner nét đứt với bảng và không có cạnh.</Li>
          </Ul>
          <H>Tương tác</H>
          <Ul>
            <Li>Chọn một node để mở bảng. Chuột phải lên node để dùng <K>Open table, Copy qualified name</K> và <K>Focus related tables</K>.</Li>
            <Li>Kéo node để sắp xếp. Vị trí được lưu theo session và schema. <K>Auto arrange</K> dựng lại layout cân bằng và lưu; <K>Reset layout</K> xóa vị trí đã lưu, dựng lại layout tự động và fit view.</Li>
            <Li>Lọc làm mờ bảng không khớp. <K>Enter</K> focus kết quả đầu. Chọn node hoặc cạnh làm mờ phần không liên quan và hiển thị thanh chi tiết.</Li>
            <Li>Cuộn hoặc pinch để zoom từ 0.1 đến 1.75. Minimap hình tròn. Điều khiển zoom và fit ở góc trái dưới.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'history',
    title: { en: 'History', vi: 'History' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>History lists executed statements from consoles as cards with <K>time, duration,</K> and <K>rows</K>, newest first, capped at 200 entries and stored in the backend application database.</P>
          <Ul>
            <Li>Filter by SQL text. Select a card to reopen its SQL, up to 2,000 characters per statement, as a new query.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>History liệt kê các câu lệnh đã chạy từ consoles dưới dạng thẻ gồm <K>time, duration</K> và <K>rows</K>, mới nhất trước, tối đa 200 mục và lưu trong database ứng dụng backend.</P>
          <Ul>
            <Li>Lọc theo nội dung SQL. Chọn một thẻ để mở lại SQL, tối đa 2.000 ký tự mỗi câu, thành query mới.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'snippets',
    title: { en: 'Snippets', vi: 'Snippets' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>Snippets is a personal SQL library stored in the backend application database.</P>
          <Ul>
            <Li>Save from the query toolbar <K>Snippet</K> button, which asks for a name.</Li>
            <Li>Select a card to reopen its SQL. The trash button deletes an entry.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Snippets là thư viện SQL cá nhân lưu trong database ứng dụng backend.</P>
          <Ul>
            <Li>Lưu từ nút <K>Snippet</K> trên toolbar query, hộp thoại hỏi tên.</Li>
            <Li>Chọn một thẻ để mở lại SQL. Nút xóa xóa một mục.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'aliases',
    title: { en: 'Aliases', vi: 'Aliases' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>Aliases are short triggers that expand in autocomplete — e.g. <K>ssf</K> → <K>SELECT * FROM</K>. The list is served by the backend with user overrides.</P>
          <Ul>
            <Li>Add via the plus button, edit via the pencil, delete custom entries via the trash button. Editing a builtin creates an override.</Li>
            <Li>Reset to defaults drops all custom entries and builtin overrides.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Aliases là trigger ngắn bung ra trong autocomplete — ví dụ <K>ssf</K> → <K>SELECT * FROM</K>. Danh sách do backend phục vụ kèm override của user.</P>
          <Ul>
            <Li>Thêm bằng nút cộng, sửa bằng bút chì, xóa entry custom bằng nút xóa. Sửa builtin tạo override.</Li>
            <Li>Reset về mặc định xóa mọi entry custom và override builtin.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'server',
    title: { en: 'Server', vi: 'Server' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>The Server view summarizes the connected instance.</P>
          <Ul>
            <Li>Version, database with size, uptime, and connections with the maximum.</Li>
            <Li>Settings grid with <K>name, setting,</K> and <K>unit</K> from the server snapshot.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>View Server tóm tắt instance đang kết nối.</P>
          <Ul>
            <Li>Version, database kèm dung lượng, uptime và connections kèm mức tối đa.</Li>
            <Li>Lưới Settings gồm <K>name, setting</K> và <K>unit</K> từ snapshot server.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'activity',
    title: { en: 'Activity', vi: 'Activity' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>Activity shows live <K>pg_stat_activity</K> rows and refreshes every 5 seconds.</P>
          <Ul>
            <Li>Grid with <K>pid, user, state, duration,</K> and <K>query</K>. Queries are trimmed to 120 characters. Select a pid to copy the number.</Li>
            <Li>Each row has <K>cancel</K> to cancel the backend and <K>kill</K> to terminate the process. Termination asks for danger confirmation and ends the process at once.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Activity hiển thị các dòng <K>pg_stat_activity</K> trực tiếp và làm mới mỗi 5 giây.</P>
          <Ul>
            <Li>Lưới gồm <K>pid, user, state, duration</K> và <K>query</K>. Query được rút gọn còn 120 ký tự. Chọn pid để copy số.</Li>
            <Li>Mỗi dòng có <K>cancel</K> để hủy backend và <K>kill</K> để kết thúc process. Kết thúc process yêu cầu xác nhận danger và dừng process ngay.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'locks',
    title: { en: 'Locks', vi: 'Locks' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>Locks shows <K>pg_locks</K> joined with <K>pg_stat_activity</K>, waiting blockers first, refreshing every 5 seconds.</P>
          <Ul>
            <Li>Counters for granted in green and waiting in red.</Li>
            <Li>Grid with <K>pid, user, locktype, relation, mode, status,</K> and <K>query</K>. Queries are trimmed to 80 characters. <K>waiting</K> renders in bold red. <K>granted</K> renders in green.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Locks hiển thị <K>pg_locks</K> join với <K>pg_stat_activity</K>, blocker đang chờ trước, làm mới mỗi 5 giây.</P>
          <Ul>
            <Li>Bộ đếm granted màu xanh lá và waiting màu đỏ.</Li>
            <Li>Lưới gồm <K>pid, user, locktype, relation, mode, status</K> và <K>query</K>. Query rút gọn còn 80 ký tự. <K>waiting</K> hiển thị đỏ đậm. <K>granted</K> hiển thị xanh lá.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'stats-panel',
    title: { en: 'Stats panel', vi: 'Stats panel' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>The Stats view has two grids.</P>
          <Ul>
            <Li>Databases with <K>name, backends, hit ratio,</K> and <K>size</K>.</Li>
            <Li>Top tables with <K>schema, table, size, live,</K> and <K>dead</K> tuples.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>View Stats gồm hai lưới.</P>
          <Ul>
            <Li>Databases gồm <K>name, backends, hit ratio</K> và <K>size</K>.</Li>
            <Li>Top tables gồm <K>schema, table, size, live</K> và <K>dead</K> tuples.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'settings',
    title: { en: 'Settings and logging', vi: 'Settings và logging' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>Logging is centralized: HTTP through middleware, queries through the Querier wrapper, transactions through the transaction logger. Steady queries log at debug, slow queries at warning, failures at error.</P>
          <Ul>
            <Li><K>Enabled</K> turns logging on or off. <K>Level</K> selects <K>debug, info, warn,</K> or <K>error</K>. Debug shows each SQL statement.</Li>
            <Li><K>HTTP requests</K> logs status and duration. <K>Queries</K> logs SQL text, session, and row counts.</Li>
            <Li><K>Slow query threshold</K> in ms marks slower queries as warnings. <K>Max entries</K> bounds the in-memory ring buffer.</Li>
            <Li><K>Save</K> stores the config. <K>Defaults</K> restores the factory config. The config persists in <K>data/logging.json</K>. Change it through the panel or API.</Li>
          </Ul>
          <H>Security and privacy</H>
          <Ul>
            <Li><K>Allow LAN access</K> applies immediately and lets other devices on the same network send requests to pglight. Authentication is not enabled yet, so keep it off on untrusted networks.</Li>
            <Li><K>Persist query history</K> controls server-side history writes. <K>Restore result snapshots</K> keeps up to 50 rows per query tab across reloads without re-running SQL. Retention is 1–365 days.</Li>
            <Li><K>Clear history</K> removes server history. <K>Clear all local data</K> removes saved tabs, snapshots, preferences, and history, then reloads while keeping live sessions connected.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Logging tập trung: HTTP qua middleware, query qua wrapper Querier, transaction qua logger transaction. Query thường log ở debug, query chậm ở warning, lỗi ở error.</P>
          <Ul>
            <Li><K>Enabled</K> bật hoặc tắt logging. <K>Level</K> chọn <K>debug, info, warn</K> hoặc <K>error</K>. Debug hiển thị từng câu SQL.</Li>
            <Li><K>HTTP requests</K> log status và duration. <K>Queries</K> log nội dung SQL, session và số dòng.</Li>
            <Li><K>Slow query threshold</K> tính bằng ms đánh dấu query chậm hơn thành warning. <K>Max entries</K> giới hạn ring buffer trong RAM.</Li>
            <Li><K>Save</K> lưu cấu hình. <K>Defaults</K> khôi phục cấu hình gốc. Cấu hình persist trong <K>data/logging.json</K>. Thay đổi qua panel hoặc API.</Li>
          </Ul>
          <H>Bảo mật và riêng tư</H>
          <Ul>
            <Li><K>Allow LAN access</K> áp dụng ngay và cho phép thiết bị cùng mạng gửi request đến pglight. Chưa có authentication, vì vậy nên tắt khi dùng mạng không tin cậy.</Li>
            <Li><K>Persist query history</K> điều khiển việc ghi history phía server. <K>Restore result snapshots</K> giữ tối đa 50 dòng mỗi query tab sau khi reload mà không chạy lại SQL. Retention từ 1–365 ngày.</Li>
            <Li><K>Clear history</K> xóa history phía server. <K>Clear all local data</K> xóa tabs, snapshots, preferences và history đã lưu rồi reload, nhưng giữ các session đang kết nối.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'logs',
    title: { en: 'Log viewer', vi: 'Log viewer' },
    group: { en: 'Right panel', vi: 'Panel phải' },
    body: {
      en: (
        <>
          <P>The log viewer shows entries newest first.</P>
          <Ul>
            <Li>Filter by level and by category (<K>http, query, txn, system</K>).</Li>
            <Li>The auto-refresh switch reloads every 2 seconds. <K>Clear</K> empties the buffer through <K>DELETE /api/logs</K>.</Li>
            <Li>When the list is empty, run a query or lower the level to debug. <K>/api/logs</K> does not log itself.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Log viewer hiển thị entries, mới nhất trước.</P>
          <Ul>
            <Li>Lọc theo level và category (<K>http, query, txn, system</K>).</Li>
            <Li>Switch auto-refresh tải lại mỗi 2 giây. <K>Clear</K> xóa buffer qua <K>DELETE /api/logs</K>.</Li>
            <Li>Khi danh sách trống, chạy một query hoặc hạ level xuống debug. <K>/api/logs</K> không tự log chính nó.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'sessions-restore',
    title: { en: 'Sessions and restore', vi: 'Phiên và khôi phục' },
    group: { en: 'System', vi: 'Hệ thống' },
    body: {
      en: (
        <>
          <P>Reloading the page returns the same workspace.</P>
          <Ul>
            <Li>Autologin replays the last connection after verifying the stored session. Sessions without a live pool are marked dead.</Li>
            <Li>Tabs are rebuilt: tables, browsers, and diagrams reload their data. Query tabs show the last result as a labeled snapshot. Select <K>Run</K> to refresh. The open tab, the autocommit switch, and layout sizes are restored.</Li>
            <Li>A heartbeat runs every 30 seconds and on window focus, with a one-shot retry. A global 401 handler returns the app to the disconnected state.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <P>Tải lại trang sẽ trở về đúng workspace.</P>
          <Ul>
            <Li>Autologin phát lại connection cuối sau khi kiểm tra session đã lưu. Session không còn pool hoạt động được gắn nhãn dead.</Li>
            <Li>Các tab được dựng lại: table, browser và diagram tải lại dữ liệu. Tab query hiển thị result cuối dưới dạng snapshot có nhãn. Chọn <K>Run</K> để làm mới. Tab đang mở, switch autocommit và kích thước layout được khôi phục.</Li>
            <Li>Heartbeat chạy mỗi 30 giây và khi focus cửa sổ, kèm một lần retry. Handler 401 toàn cục đưa app về trạng thái disconnected.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'shortcuts',
    title: { en: 'Keyboard shortcuts', vi: 'Phím tắt' },
    group: { en: 'System', vi: 'Hệ thống' },
    body: {
      en: (
        <>
          <Ul>
            <Li><K>Ctrl/Command + Enter</K>: run the query in the active console.</Li>
            <Li><K>Ctrl/Command + K</K>: global search. <K>Enter</K> opens the top hit.</Li>
            <Li><K>Ctrl + Space</K>: open completion. <K>Tab</K> or <K>Enter</K>: accept it.</Li>
            <Li><K>Enter</K>: confirm dialogs and the palette input. <K>Esc</K>: close search and dialogs.</Li>
            <Li><K>Middle-click</K>: close a tab. <K>Double-click</K> a cell: edit the value.</Li>
            <Li><K>Ctrl/Command-click, Shift-click</K>: multi-select rows in Data.</Li>
            <Li><K>Right-click</K>: copy a cell, row menu, explorer and tab menus.</Li>
            <Li>Open Workspace → <K>Shortcuts</K> to search commands, record a new binding, replace conflicts with confirmation, reset one command, or reset all. Overrides are validated and saved per app user; browser-reserved bindings are marked.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <Ul>
            <Li><K>Ctrl/Command + Enter</K>: chạy query ở console đang mở.</Li>
            <Li><K>Ctrl/Command + K</K>: tìm kiếm toàn cục. <K>Enter</K> mở kết quả đầu.</Li>
            <Li><K>Ctrl + Space</K>: mở gợi ý. <K>Tab</K> hoặc <K>Enter</K>: nhận gợi ý.</Li>
            <Li><K>Enter</K>: xác nhận dialogs và ô palette. <K>Esc</K>: đóng search và dialogs.</Li>
            <Li><K>Middle-click</K>: đóng tab. <K>Double-click</K> cell: sửa giá trị.</Li>
            <Li><K>Ctrl/Command-click, Shift-click</K>: chọn nhiều dòng trong Data.</Li>
            <Li><K>Right-click</K>: copy cell, menu dòng, menu explorer và tab.</Li>
            <Li>Mở Workspace → <K>Shortcuts</K> để tìm command, ghi binding mới, thay binding xung đột sau khi xác nhận, reset từng command hoặc reset tất cả. Override được validate và lưu theo app user; binding bị browser giữ chỗ sẽ được đánh dấu.</Li>
          </Ul>
        </>
      ),
    },
  },
  {
    id: 'safety',
    title: { en: 'Safety and limits', vi: 'An toàn và giới hạn' },
    group: { en: 'System', vi: 'Hệ thống' },
    body: {
      en: (
        <>
          <Ul>
            <Li>The server refuses UPDATE and DELETE without a WHERE clause. Cell edits require a primary key. NULL primary key values block the edit.</Li>
            <Li>Maintenance permits <K>vacuum, vacuum_full, analyze,</K> and <K>reindex</K> only, and refuses them inside an open transaction.</Li>
            <Li>Import is capped at 20,000 rows in batches of 500 and runs atomically. Identifiers are sanitized with <K>pgx.Identifier</K>. Values use <K>$n</K> parameters. The <K>/api/table-data</K> filter and order form the one exception, with identifier sanitize plus a sort-direction list.</Li>
            <Li>Result sets are capped at 1,000 rows. Explorer queries use LIMIT. Contexts last 5-30 seconds, 120 seconds for maintenance and import.</Li>
            <Li>Success responses keep existing shapes and only gain fields. Errors use <K>{'{error}'}</K>. Query-like responses include <K>in_txn</K>.</Li>
          </Ul>
        </>
      ),
      vi: (
        <>
          <Ul>
            <Li>Server từ chối UPDATE và DELETE không có mệnh đề WHERE. Sửa cell cần primary key. Giá trị primary key NULL sẽ chặn sửa.</Li>
            <Li>Maintenance chỉ cho <K>vacuum, vacuum_full, analyze</K> và <K>reindex</K>, và từ chối trong transaction đang mở.</Li>
            <Li>Import giới hạn 20.000 dòng theo batch 500 và chạy nguyên tử. Identifier được sanitize bằng <K>pgx.Identifier</K>. Giá trị dùng tham số <K>$n</K>. Filter và order của <K>/api/table-data</K> là ngoại lệ duy nhất, với sanitize identifier kèm danh sách hướng sort.</Li>
            <Li>Result sets giới hạn 1.000 dòng. Query explorer dùng LIMIT. Context 5-30 giây, 120 giây cho maintenance và import.</Li>
            <Li>Response thành công giữ shape hiện có và chỉ thêm field. Lỗi dùng <K>{'{error}'}</K>. Response dạng query gồm <K>in_txn</K>.</Li>
          </Ul>
        </>
      ),
    },
  },
]

export function DocsView() {
  const [filter, setFilter] = useState('')
  const [lang, setLang] = useAppPreference<DocsLang>('docs-lang', 'en')
  const [active, setActive] = useState(SECTIONS[0].id)
  const t = UI[lang]
  const list = useMemo(() => {
    const f = filter.trim().toLowerCase()
    if (!f) return SECTIONS
    return SECTIONS.filter((s) => (s.title.en + ' ' + s.title.vi + ' ' + textOf(s.id)).toLowerCase().includes(f))
  }, [filter])

  const jump = (id: string) => {
    setActive(id)
    document.getElementById(`docs-${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  // Scroll-spy: highlight the section currently in view.
  useEffect(() => {
    const els = list
      .map((s) => document.getElementById(`docs-${s.id}`))
      .filter((el): el is HTMLElement => el != null)
    if (!els.length) return
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) setActive(e.target.id.replace(/^docs-/, ''))
        }
      },
      { rootMargin: '-10% 0px -75% 0px' },
    )
    els.forEach((el) => obs.observe(el))
    return () => obs.disconnect()
  }, [list])

  const visible: Record<string, boolean> = {}
  for (const s of list) visible[s.id] = true

  return (
    <div className="mx-auto flex max-w-[1060px] gap-4">
      {/* Vertical TOC, sticky on the left */}
      <nav aria-label={t.toc} className="sticky top-0 hidden w-[228px] shrink-0 self-start md:block">
        <ScrollArea className="max-h-[calc(100vh-140px)] pr-2">
          <div className="flex flex-col gap-3 py-1">
            {GROUP_ORDER[lang].map((g) => {
              const items = SECTIONS.filter((s) => s.group[lang] === g && visible[s.id])
              if (!items.length) return null
              return (
                <div key={g}>
                  <div className="px-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">{g}</div>
                  <div className="flex flex-col gap-px">
                    {items.map((s) => (
                      <button
                        key={s.id}
                        onClick={() => jump(s.id)}
                        aria-current={s.id === active ? 'true' : undefined}
                        className={cn(
                          'rounded-md px-2 py-1 text-left text-[12px] transition-colors hover:bg-accent hover:text-foreground',
                          s.id === active ? 'bg-accent font-semibold text-foreground' : 'text-muted-foreground',
                        )}
                      >
                        {s.title[lang]}
                      </button>
                    ))}
                  </div>
                </div>
              )
            })}
          </div>
        </ScrollArea>
      </nav>
      {/* Content */}
      <div className="flex min-w-0 flex-1 flex-col gap-3">
        <div className="flex items-center gap-2">
          <BookOpen className="h-4 w-4" />
          <b className="text-sm">{t.docs}</b>
          <span className="text-[12px] text-muted-foreground">{list.length}/{SECTIONS.length}</span>
          <span className="flex-1" />
          <div className="flex overflow-hidden rounded-md border" role="group" aria-label={t.langLabel}>
            {(['en', 'vi'] as DocsLang[]).map((l) => (
              <Button
                key={l}
                size="sm"
                variant={lang === l ? 'secondary' : 'ghost'}
                aria-pressed={lang === l}
                onClick={() => setLang(l)}
                className="rounded-none border-0 px-2.5 font-mono text-[11px] uppercase first:rounded-l-md last:rounded-r-md"
              >
                {l}
              </Button>
            ))}
          </div>
          <Input className="w-[220px]" placeholder={t.filter} value={filter} onChange={(e) => setFilter(e.target.value)} />
        </div>
        {list.map((s) => (
          <Card key={s.id} id={`docs-${s.id}`} className="scroll-mt-2 p-3.5">
            <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">{s.group[lang]}</div>
            <div className="mb-1.5 text-[13px] font-semibold">{s.title[lang]}</div>
            {s.body[lang]}
          </Card>
        ))}
        {!list.length && <Card className="p-6 text-center text-muted-foreground">{t.empty}</Card>}
      </div>
    </div>
  )
}

function textOf(id: string): string {
  const hints: Record<string, string> = {
    connect: 'connect login password saved auto-connect disconnect database session reconnect dead sslmode tls profile folder tags favorite default duplicate test vault reset password-stdin master password recovery kết nối phiên lưu đăng nhập tls profile folder tag vault khôi phục reset',
    explorer: 'tree tables views matviews foreign functions sequences types extensions roles filter definition right-click context menu cây duyệt đối tượng lọc',
    search: 'ctrl k palette global search tables columns debounce tìm kiếm toàn cục bảng cột',
    tabs: 'tab layout resizable split right down swap pane middle-click close dirty active badge thẻ bố cục chia pane đổi đóng',
    query: 'sql run multi-statement explain analyze format snippet export csv json insert transaction autocommit autocomplete codemirror limit cancel truy vấn chạy gợi ý',
    txn: 'transaction begin commit rollback autocommit in_txn giao dịch',
    'table-data': 'data where order paging edit delete insert csv import bulk select null primary key dữ liệu dòng nhập sửa xóa',
    'mock-data': 'mock generate preview simple advanced seed generator datatype fk foreign key check unique null probability dữ liệu mẫu sinh xem trước',
    'table-structure': 'columns add rename type nullable default alter structure cột cấu trúc thêm đổi tên kiểu',
    'table-indexes': 'index unique btree gin gist brin include partial where keys expression chỉ mục',
    'table-constraints': 'constraint check unique primary key foreign key exclude ràng buộc',
    'table-triggers': 'trigger before after instead of row statement function update of when trigger',
    'table-sql': 'ddl create table definition copy sql',
    'table-stats': 'stats vacuum analyze reindex maintenance size scans tuples thống kê bảo trì',
    objects: 'function sequence type enum properties definition overload regprocedure đối tượng hàm chuỗi kiểu',
    browser: 'extensions roles browser grid tiện ích vai trò trình duyệt',
    erd: 'diagram erd foreign key graph schema canvas node edge layout minimap line bezier straight smoothstep sơ đồ quan hệ',
    history: 'history executed statements reopen filter lịch sử câu lệnh',
    aliases: 'aliases trigger expansion autocomplete ssf prefix override builtin reset trigger mở rộng gợi ý',
    snippets: 'snippets saved sql library delete đoạn mã lưu',
    server: 'server version uptime connections settings máy chủ phiên bản',
    activity: 'activity pg_stat_activity cancel kill pid backend sessions hoạt động tiến trình hủy',
    locks: 'locks pg_locks blockers granted waiting khóa blocker chờ',
    'stats-panel': 'stats databases hit ratio top tables thống kê cơ sở dữ liệu',
    settings: 'settings logging aop level debug slow threshold max entries security lan privacy history snapshots retention clear config cài đặt nhật ký bảo mật riêng tư',
    logs: 'logs viewer level category http query txn system clear nhật ký xem',
    'sessions-restore': 'restore reopen tabs autologin heartbeat persist reload khôi phục phiên',
    shortcuts: 'keyboard shortcuts command rebind conflict reset ctrl enter escape search phím tắt lệnh đổi phím',
    safety: 'safety where primary key maintenance import limit sanitize param an toàn',
  }
  return hints[id] ?? ''
}
