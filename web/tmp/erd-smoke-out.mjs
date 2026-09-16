// src/components/erd/erdMapper.ts
var ERD_MAX_COLUMNS = 30;

// src/components/erd/erdLayout.ts
var ERD_NODE_WIDTH = 264;
var X_GAP = 90;
var Y_GAP = 48;
var ROW_H = 20;
var HEADER_H = 57;
function estimateHeight(colCount) {
  const rows = Math.min(Math.max(colCount, 1), ERD_MAX_COLUMNS);
  return HEADER_H + rows * ROW_H + (colCount > ERD_MAX_COLUMNS ? 20 : 0);
}
function layoutTables(tables2, relations, saved) {
  const out = {};
  if (!tables2.length) return out;
  const byId = new Map(tables2.map((t) => [t.id, t]));
  const height = new Map(tables2.map((t) => [t.id, estimateHeight(t.columns.length)]));
  const parent = new Map(tables2.map((t) => [t.id, t.id]));
  const find = (x) => {
    const p = parent.get(x) ?? x;
    if (p === x) return x;
    const r = find(p);
    parent.set(x, r);
    return r;
  };
  const adj = /* @__PURE__ */ new Map();
  for (const t of tables2) adj.set(t.id, /* @__PURE__ */ new Set());
  for (const r of relations) {
    if (!byId.has(r.sourceTable) || !byId.has(r.targetTable)) continue;
    adj.get(r.sourceTable)?.add(r.targetTable);
    adj.get(r.targetTable)?.add(r.sourceTable);
    const a = find(r.sourceTable);
    const b = find(r.targetTable);
    if (a !== b) parent.set(a, b);
  }
  const comps = /* @__PURE__ */ new Map();
  for (const t of tables2) {
    const root = find(t.id);
    comps.set(root, [...comps.get(root) ?? [], t.id]);
  }
  const degree = (id) => adj.get(id)?.size ?? 0;
  const connected = [...comps.values()].filter((c) => c.length > 1 || degree(c[0] ?? "") > 0);
  connected.sort((a, b) => b.length - a.length || (a[0] ?? "").localeCompare(b[0] ?? ""));
  const isolated = [...comps.values()].filter((c) => c.length === 1 && degree(c[0] ?? "") === 0).map((c) => c[0] ?? "").sort((a, b) => a.localeCompare(b));
  const blocks = connected.map((comp) => layoutConnected(comp, adj, height));
  if (isolated.length) blocks.push(layoutGrid(isolated, height));
  const totalArea = blocks.reduce((sum, b) => sum + b.w * b.h, 0);
  const maxW = blocks.reduce((m, b) => Math.max(m, b.w), 0);
  const rowWidth = Math.max(maxW, Math.ceil(Math.sqrt(Math.max(totalArea, 0))));
  let cursorX = 0;
  let cursorY = 0;
  let rowH = 0;
  for (const block of blocks) {
    if (cursorX > 0 && cursorX + block.w > rowWidth) {
      cursorX = 0;
      cursorY += rowH + Y_GAP * 2;
      rowH = 0;
    }
    for (const [id, p] of block.local) {
      out[id] = { x: cursorX + p.x, y: cursorY + p.y };
    }
    cursorX += block.w + X_GAP;
    rowH = Math.max(rowH, block.h);
  }
  return { ...out, ...pickSaved(saved, byId) };
}
function layoutConnected(comp, adj, height) {
  const degree = (id) => adj.get(id)?.size ?? 0;
  const start = [...comp].sort((a, b) => degree(b) - degree(a) || a.localeCompare(b))[0] ?? comp[0] ?? "";
  const depth = /* @__PURE__ */ new Map([[start, 0]]);
  const queue = [start];
  while (queue.length) {
    const cur = queue.shift();
    for (const nb of adj.get(cur) ?? []) {
      if (!depth.has(nb)) {
        depth.set(nb, (depth.get(cur) ?? 0) + 1);
        queue.push(nb);
      }
    }
  }
  const maxDepth = depth.size ? Math.max(...depth.values()) : 0;
  for (const id of comp) if (!depth.has(id)) depth.set(id, maxDepth + 1);
  const layers = /* @__PURE__ */ new Map();
  for (const id of comp) {
    const d = depth.get(id) ?? 0;
    layers.set(d, [...layers.get(d) ?? [], id]);
  }
  for (const ids of layers.values()) ids.sort((a, b) => degree(b) - degree(a) || a.localeCompare(b));
  const local = /* @__PURE__ */ new Map();
  let layerX = 0;
  let compH = 0;
  for (const [d, ids] of [...layers.entries()].sort((a, b) => a[0] - b[0])) {
    const grid = layoutGrid(ids, height);
    for (const [id, p] of grid.local) {
      local.set(id, { x: layerX + p.x, y: p.y });
    }
    layerX += grid.w + X_GAP;
    compH = Math.max(compH, grid.h);
  }
  const compW = layers.size ? layerX - X_GAP : 0;
  return { local, w: compW, h: compH };
}
function layoutGrid(ids, height) {
  const n = ids.length;
  if (!n) return { local: /* @__PURE__ */ new Map(), w: 0, h: 0 };
  const cols = Math.max(1, Math.ceil(Math.sqrt(n)));
  const colY = new Array(cols).fill(0);
  const local = /* @__PURE__ */ new Map();
  ids.forEach((id, i) => {
    const c = i % cols;
    const x = c * (ERD_NODE_WIDTH + X_GAP);
    const y = colY[c] ?? 0;
    local.set(id, { x, y });
    colY[c] = y + (height.get(id) ?? 120) + Y_GAP;
  });
  const w = cols * (ERD_NODE_WIDTH + X_GAP) - X_GAP;
  const h = Math.max(0, ...colY) - Y_GAP;
  return { local, w, h: Math.max(h, 0) };
}
function pickSaved(saved, byId) {
  const out = {};
  for (const [id, p] of Object.entries(saved)) {
    if (byId.has(id) && Number.isFinite(p?.x) && Number.isFinite(p?.y)) out[id] = { x: p.x, y: p.y };
  }
  return out;
}

// erd-smoke.mjs
var mkTables = (n) => Array.from({ length: n }, (_, i) => ({ id: `t${i}`, schema: "public", name: `t${i}`, columns: [{ name: "id" }], ports: {} }));
function checkOverlap(tables2, pos2) {
  const W = 264;
  for (let i = 0; i < tables2.length; i++) {
    for (let j = i + 1; j < tables2.length; j++) {
      const a = pos2[tables2[i].id], b = pos2[tables2[j].id];
      const ha = estimateHeight(tables2[i].columns.length), hb = estimateHeight(tables2[j].columns.length);
      const ox = Math.max(0, Math.min(a.x + W, b.x + W) - Math.max(a.x, b.x));
      const oy = Math.max(0, Math.min(a.y + ha, b.y + hb) - Math.max(a.y, b.y));
      if (ox > 0 && oy > 0) return `${tables2[i].id} overlaps ${tables2[j].id}`;
    }
  }
  return null;
}
function bounds(tables2, pos2) {
  let mx = 0, my = 0;
  for (const t of tables2) {
    const p = pos2[t.id];
    mx = Math.max(mx, p.x);
    my = Math.max(my, p.y + estimateHeight(t.columns.length));
  }
  return { mx, my };
}
var tables = mkTables(24);
var pos = layoutTables(tables, [], {});
var xs = new Set(Object.values(pos).map((p) => p.x));
console.log("isolated24: distinctX=", xs.size, "bounds=", JSON.stringify(bounds(tables, pos)), "overlap=", checkOverlap(tables, pos) ?? "none");
tables = mkTables(16);
var rels = Array.from({ length: 15 }, (_, i) => ({ id: `e${i}`, fk: `fk${i}`, sourceTable: `t${i + 1}`, sourceColumn: "id", targetSchema: "public", targetTable: "t0", targetColumn: "id", labeled: false }));
pos = layoutTables(tables, rels, {});
console.log("star16: bounds=", JSON.stringify(bounds(tables, pos)), "overlap=", checkOverlap(tables, pos) ?? "none");
tables = mkTables(10);
var chain = Array.from({ length: 9 }, (_, i) => ({ id: `c${i}`, fk: `fk${i}`, sourceTable: `t${i + 1}`, sourceColumn: "id", targetSchema: "public", targetTable: `t${i}`, targetColumn: "id", labeled: false }));
pos = layoutTables(tables, chain, {});
console.log("chain10: bounds=", JSON.stringify(bounds(tables, pos)), "overlap=", checkOverlap(tables, pos) ?? "none");
pos = layoutTables(mkTables(3), [], { t1: { x: 9999, y: 8888 } });
console.log("saved:", JSON.stringify(pos.t1), "count=", Object.keys(pos).length);
