let session=localStorage.getItem('sid')||'';
let treeData=[],tabs=[],activeTab=null,tabSeq=0,history=JSON.parse(localStorage.getItem('hist')||'[]');
let compl={tables:[],columns:[],keywords:[]};
const $=id=>document.getElementById(id);
const api=(p,o={})=>fetch(p,o).then(r=>r.json());
const sid=q=>(q.includes('?')?'&':'?')+'session_id='+session;

function status(ok){const s=$('status');s.textContent=ok?'connected':'disconnected';s.className=ok?'ok':''}

// --- connections (pgAdmin-style saved servers) ---
function savedList(){return JSON.parse(localStorage.getItem('conns')||'[]')}
function refreshSaved(){const s=$('saved');s.innerHTML='<option value="">— saved —</option>';savedList().forEach((c,i)=>{const o=document.createElement('option');o.value=i;o.textContent=c.name; s.appendChild(o)});s.onchange=()=>{const c=savedList()[+s.value];if(!c)return;['host','port','user','password','dbname','sslmode'].forEach(k=>$(k).value=c[k]??$(k).value)}}
function saveConn(){const c={name:prompt('name?', $('host').value+'/'+$('dbname').value)||'conn',host:$('host').value,port:$('port').value,user:$('user').value,password:$('password').value,dbname:$('dbname').value,sslmode:$('sslmode').value};const l=savedList();l.push(c);localStorage.setItem('conns',JSON.stringify(l));refreshSaved()}
async function connect(fresh){const body={host:$('host').value,port:+$('port').value,user:$('user').value,password:$('password').value,dbname:$('dbname').value,sslmode:$('sslmode').value,session_id:fresh?'':session};const j=await api('/api/connect',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(j.error)return alert(j.error);session=j.session_id;localStorage.setItem('sid',session);status(true);loadExplorer();loadComplete();if(!tabs.length)newQueryTab()}
function disconnect(){api('/api/disconnect'+sid('?'));session='';localStorage.removeItem('sid');status(false)}

// --- explorer (DataGrip-like tree) ---
async function loadExplorer(){
  if(!session)return;
  const schemas=await api('/api/schemas'+sid('?'));
  const tables=await api('/api/tables'+sid('?'));
  const bySchema={};
  (tables||[]).forEach(t=>{(bySchema[t.schema]=bySchema[t.schema]||[]).push(t)});
  treeData=(schemas||[]).map(s=>({schema:s,kids:(bySchema[s]||[])}));
  renderTree();
}
function renderTree(){
  const f=($('treeFilter').value||'').toLowerCase(),el=$('tree');el.innerHTML='';
  treeData.forEach(s=>{
    const kids=s.kids.filter(t=>(s.schema+'.'+t.name).toLowerCase().includes(f));
    if(!kids.length)return;
    const d=document.createElement('div');d.innerHTML=`<div class="schema">▾ ${s.schema} (${kids.length})</div>`;
    kids.forEach(t=>{
      const r=document.createElement('div');r.className='tbl';r.textContent=`${t.type==='VIEW'?'👁':'▦'} ${t.name} ~${t.est_rows}`;
      r.onclick=()=>openTableTab(t.schema,t.name);
      r.ondblclick=()=>showDDL(t.schema,t.name);
      d.appendChild(r);
    });
    el.appendChild(d);
  });
}
async function showDDL(schema,table){
  const j=await api(`/api/ddl${sid('?')}&schema=${schema}&table=${table}`);
  $('objBody').innerHTML=j.error?`<span class="err">${j.error}</span>`:
    `<b>${schema}.${table}</b><pre class="ddl">${esc(j.ddl)}</pre><b>Indexes</b><pre class="ddl">${esc((j.indexes||[]).map(i=>i.def).join('\n'))}</pre><b>FKs</b><pre class="ddl">${esc((j.foreign_keys||[]).map(i=>i.def).join('\n'))}</pre><div class="row"><button onclick="openTableTab('${schema}','${table}')">Browse</button></div>`;
}
function esc(s){return (s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;')}

// --- tabs ---
function newQueryTab(sql='SELECT * FROM information_schema.tables LIMIT 20;'){
  const id='q'+(++tabSeq);tabs.push({id,kind:'query',title:'Query '+tabSeq,sql,limit:200,result:null});
  activeTab=id;renderTabs();
}
function openTableTab(schema,table){
  const id='t_'+schema+'_'+table;
  if(!tabs.find(t=>t.id===id))tabs.push({id,kind:'table',title:table,schema,table,limit:100,offset:0,filter:'',order:'',result:null});
  activeTab=id;renderTabs();loadTablePage(id);
}
function closeTab(e,id){e.stopPropagation();tabs=tabs.filter(t=>t.id!==id);if(activeTab===id)activeTab=tabs[0]?.id;renderTabs()}
function renderTabs(){
  $('tabs').innerHTML='';$('pages').innerHTML='';
  tabs.forEach(t=>{
    const d=document.createElement('div');d.className='tab'+(t.id===activeTab?' active':'');d.innerHTML=`${esc(t.title)}<span onclick="closeTab(event,'${t.id}')">✖</span>`;
    d.onclick=()=>{activeTab=t.id;renderTabs()};$('tabs').appendChild(d);
    const p=document.createElement('div');p.className='page'+(t.id===activeTab?' active':'');p.id='p_'+t.id;
    p.innerHTML=t.kind==='query'?queryPageHTML(t):tablePageHTML(t);
    $('pages').appendChild(p);
    if(t.result)paintResult(t);
  });
}
function cur(){return tabs.find(t=>t.id===activeTab)}
function queryPageHTML(t){
  return `<textarea class="sql" id="sql_${t.id}" list="compl" spellcheck="false">${esc(t.sql)}</textarea>
  <div class="row"><button onclick="runQuery('${t.id}')">Run (Ctrl+Enter)</button>
  <button class="ghost" onclick="explain('${t.id}',false)">Explain</button>
  <button class="ghost" onclick="explain('${t.id}',true)">Analyze</button>
  <select id="lim_${t.id}"><option ${t.limit===200?'selected':''}>200</option><option ${t.limit===1000?'selected':''}>1000</option><option value="0">no limit</option></select>
  <button class="ghost" onclick="exportCSV('${t.id}')">CSV</button>
  <button class="ghost" onclick="exportJSON('${t.id}')">JSON</button>
  <span class="meta" id="meta_${t.id}"></span></div>
  <div class="err" id="err_${t.id}"></div><div id="plan_${t.id}"></div><div style="overflow:auto"><table class="grid" id="grid_${t.id}"></table></div>`;
}
function tablePageHTML(t){
  return `<div class="row"><b>${esc(t.schema+'.'+t.table)}</b>
  <input id="flt_${t.id}" value="${esc(t.filter)}" placeholder="WHERE … e.g. id > 10" size="28">
  <input id="ord_${t.id}" value="${esc(t.order)}" placeholder="ORDER … e.g. id DESC" size="16">
  <button onclick="loadTablePage('${t.id}')">Apply</button>
  <button class="ghost" onclick="showDDL('${t.schema}','${t.table}')">DDL</button>
  <button class="ghost" onclick="insertRow('${t.id}')">+ Row</button>
  <button class="ghost" onclick="exportCSV('${t.id}')">CSV</button>
  <span class="meta" id="meta_${t.id}"></span></div>
  <div class="row"><button onclick="page('${t.id}',-1)">←</button><span id="pg_${t.id}"></span><button onclick="page('${t.id}',1)">→</button></div>
  <div class="err" id="err_${t.id}"></div><div style="overflow:auto"><table class="grid" id="grid_${t.id}"></table></div>`;
}

// --- query exec + history + explain (DataGrip core) ---
async function runQuery(id){
  const t=tabs.find(x=>x.id===id);t.sql=$('sql_'+id).value;t.limit=+$('lim_'+id).value||0;
  $('err_'+id).textContent='';$('plan_'+id).innerHTML='';
  const st=performance.now();
  const j=await api('/api/query',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({session_id:session,sql:t.sql,limit:t.limit||undefined})});
  if(j.error){$('err_'+id).textContent=j.error;return}
  t.result=j;pushHist(t.sql,j.duration_ms,j.rows.length);paintResult(t);
  $('meta_'+id).textContent=`${j.rows.length} rows · ${j.duration_ms}ms · ${(performance.now()-st).toFixed(0)}ms total`;
}
async function explain(id,analyze){
  const t=cur()||tabs.find(x=>x.id===id);const sql=$('sql_'+id)?.value||t.sql;
  const j=await api('/api/explain',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({session_id:session,sql,analyze})});
  if(j.error)return $('err_'+id).textContent=j.error;
  try{const plan=j[0].Plan;$('plan_'+id).innerHTML=`<div class="plan">${esc(fmtPlan(plan,0))}\nPlanning: ${j[0]['Planning Time']}ms${j[0]['Execution Time']?` · Exec: ${j[0]['Execution Time']}ms`:''}</div>`}
  catch(e){$('plan_'+id).innerHTML=`<pre>${esc(JSON.stringify(j,null,2))}</pre>`}
}
function fmtPlan(n,d){return '  '.repeat(d)+`→ ${n['Node Type']} on ${n['Relation Name']||''} cost=${n['Total Cost']} rows=${n['Actual Rows']??n['Plan Rows']}\n`+(n.Plans||[]).map(p=>fmtPlan(p,d+1)).join('')}
function paintResult(t){
  const g=$('grid_'+t.id);if(!g||!t.result)return;g.innerHTML='';
  const {columns,rows}=t.result;const hr=document.createElement('tr');
  columns.forEach((c,i)=>{const th=document.createElement('th');th.textContent=c+' ⇅';th.onclick=()=>sortGrid(t,i);hr.appendChild(th)});
  g.appendChild(hr);
  rows.forEach(r=>{const tr=document.createElement('tr');r.forEach(c=>{const td=document.createElement('td');td.textContent=c==null?'NULL':String(c);td.title=td.textContent;tr.appendChild(td)});g.appendChild(tr)});
}
function sortGrid(t,i){t.result.rows.sort((a,b)=>String(a[i]??'').localeCompare(String(b[i]??'')));paintResult(t)}
function pushHist(sql,ms,n){history.unshift({sql:sql.slice(0,500),ms,n,at:new Date().toLocaleTimeString()});history=history.slice(0,200);localStorage.setItem('hist',JSON.stringify(history))}
function exportCSV(id){const t=tabs.find(x=>x.id===id);if(!t.result)return;const {columns,rows}=t.result;const csv=[columns.join(',')].concat(rows.map(r=>r.map(c=>`"${String(c??'').replace(/"/g,'""')}"`).join(','))).join('\n');dl(csv,'result.csv','text/csv')}
function exportJSON(id){const t=tabs.find(x=>x.id===id);if(!t.result)return;dl(JSON.stringify(t.result.rows.map(r=>Object.fromEntries(t.result.columns.map((c,i)=>[c,r[i]]))),null,2),'result.json','application/json')}
function dl(s,name,type){const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([s],{type}));a.download=name;a.click()}

// --- table data editor (pgAdmin-style) ---
async function loadTablePage(id){
  const t=tabs.find(x=>x.id===id);
  if($('flt_'+id)){t.filter=$('flt_'+id).value;t.order=$('ord_'+id).value}
  const j=await api(`/api/table-data${sid('?')}&schema=${t.schema}&table=${t.table}&limit=${t.limit}&offset=${t.offset}&filter=${encodeURIComponent(t.filter)}&order=${encodeURIComponent(t.order)}`);
  if(j.error){const e=$('err_'+id);if(e)e.textContent=j.error;return}
  t.result=j;paintEditable(t);
  const m=$('meta_'+id);if(m)m.textContent=`${j.rows.length} rows${j.total!=null?' / '+j.total+' total':''} · ${j.duration_ms}ms`;
  const pg=$('pg_'+id);if(pg)pg.textContent=`offset ${t.offset}`;
}
function page(id,d){const t=tabs.find(x=>x.id===id);t.offset=Math.max(0,t.offset+d*t.limit);loadTablePage(id)}
function paintEditable(t){
  const g=$('grid_'+t.id);g.innerHTML='';const {columns,rows}=t.result;
  const hr=document.createElement('tr');columns.forEach(c=>{const th=document.createElement('th');th.textContent=c;hr.appendChild(th)});
  hr.insertAdjacentHTML('beforeend','<th>✎</th>');g.appendChild(hr);
  rows.forEach(r=>{
    const tr=document.createElement('tr');const orig=Object.fromEntries(columns.map((c,i)=>[c,r[i]]));
    r.forEach((c,i)=>{
      const td=document.createElement('td');td.className='editable';td.textContent=c==null?'NULL':String(c);
      td.ondblclick=()=>{
        const v=prompt(`Edit ${columns[i]} (type __NULL__ for NULL):`,c==null?'NULL':String(c));if(v==null)return;
        rowOp(t,{[columns[i]]:v},orig).then(()=>loadTablePage(t.id));
      };
      tr.appendChild(td);
    });
    const del=document.createElement('td');del.innerHTML='<button class="ghost">del</button>';
    del.onclick=()=>{if(confirm('Delete row?'))rowOp(t,null,orig,'delete').then(()=>loadTablePage(t.id))};
    tr.appendChild(del);g.appendChild(tr);
  });
}
async function rowOp(t,values,where,op){
  op=op||(values&&where?'update':'insert');
  const j=await api('/api/row',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({session_id:session,schema:t.schema,table:t.table,op,values:values||{},where:where||{}})});
  if(j.error)alert(j.error);return j;
}
async function insertRow(id){
  const t=tabs.find(x=>x.id===id);const cols=t.result.columns;
  const vals={};for(const c of cols){const v=prompt(c+' (empty=cancel, __NULL__=NULL):','');if(v==null)return;if(v!=='')vals[c]=v}
  await rowOp(t,vals,{},'insert');loadTablePage(id);
}

// --- side panels: history + activity (pgAdmin dashboard) ---
function togglePanel(which){
  const s=$('side');
  if(s.classList.contains('hidden')){s.classList.remove('hidden');if(which)showSide(which);else showSide('history')}
  else{if(!which||$('sideTitle').textContent.toLowerCase()===which){s.classList.add('hidden')}else showSide(which)}
}
function showSide(which){$('sideTitle').textContent=which;$('sideBody').innerHTML='';
  if(which==='history'){history.forEach(h=>{const d=document.createElement('div');d.className='hist';d.innerHTML=`<small>${h.at} · ${h.ms}ms · ${h.n} rows</small><pre>${esc(h.sql.slice(0,200))}</pre>`;d.onclick=()=>{newQueryTab(h.sql)};$('sideBody').appendChild(d)})}
}
async function showActivity(){
  togglePanel();$('sideTitle').textContent='activity';const b=$('sideBody');b.innerHTML='loading…';
  const j=await api('/api/activity'+sid('?'));
  if(j.error){b.textContent=j.error;return}
  b.innerHTML='<table class="grid"><tr><th>pid</th><th>user</th><th>state</th><th>duration</th><th>query</th><th></th></tr></table>';
  const tbl=b.firstChild;
  j.forEach(r=>{
    const tr=document.createElement('tr');tr.innerHTML=`<td>${r.pid}</td><td>${esc(r.user)}</td><td>${esc(r.state)}</td><td>${esc(r.duration)}</td><td>${esc(r.query)}</td>`;
    const td=document.createElement('td');
    td.innerHTML=`<button class="ghost">cancel</button> <button class="ghost">kill</button>`;
    td.children[0].onclick=()=>api(`/api/cancel${sid('?')}&pid=${r.pid}`).then(showActivity);
    td.children[1].onclick=()=>{if(confirm('Terminate '+r.pid+'?'))api(`/api/cancel${sid('?')}&pid=${r.pid}&kill=1`).then(showActivity)};
    tr.appendChild(td);tbl.appendChild(tr);
  });
}

// --- autocomplete ---
async function loadComplete(){
  const j=await api('/api/complete'+sid('?'));if(j.error)return;compl=j;
  $('compl').innerHTML=[...j.tables,...j.columns,...j.keywords].map(v=>`<option value="${esc(v)}">`).join('');
}
document.addEventListener('keydown',e=>{if(e.ctrlKey&&e.key==='Enter'&&activeTab)runQuery(activeTab)});

refreshSaved();status(!!session);
if(session){loadExplorer();loadComplete()}
newQueryTab();
