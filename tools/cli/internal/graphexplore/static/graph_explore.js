// graph_explore.js — Canvas layer for Memory Graph Explorer
// Handles: Sigma.js, graphology, ForceAtlas2, layout, node rendering,
//          expand/collapse, search, load-by-type, filter visibility.
// UI chrome (panels, filter lists, detail content) is rendered server-side
// via templ + HTMX. This JS communicates via custom DOM events.

import Graph from 'graphology';
import { circular, random, circlepack } from 'graphology-layout';
import forceAtlas2 from 'graphology-layout-forceatlas2';
import noverlap from 'graphology-layout-noverlap';
import Sigma from 'sigma';

// ── Project context (injected by Go template) ─────────────────────────────
const PROJECT_ID = window.__PROJECT_ID;

// ── Palette ───────────────────────────────────────────────────────────────
const PALETTE = [
  '#58a6ff','#bc8cff','#3fb950','#d29922','#f78166',
  '#79c0ff','#ffa657','#ff7b72','#56d364','#e3b341',
  '#a5d6ff','#d2a8ff','#7ee787','#f0883e','#ff9492',
];
const typeColorCache = {};
let paletteIdx = 0;

function typeColor(type) {
  if (!typeColorCache[type]) typeColorCache[type] = PALETTE[paletteIdx++ % PALETTE.length];
  return typeColorCache[type];
}

// Schema type config — populated from HTMX-loaded filter items
const schemaTypeConfig = {};
const schemaTypeMeta = {};
const relLabels = {};

function typeIcon(type) {
  const cfg = schemaTypeConfig[type];
  if (cfg?.icon) return cfg.icon;
  return (type || '?').charAt(0).toUpperCase();
}

// ── State ─────────────────────────────────────────────────────────────────
const graph = new Graph({ multi: false, allowSelfLoops: false });
let sigmaInstance = null;
let selectedNode = null;
let nodeData = {};
let edgeData = {};
const hiddenNodeTypes = new Set();
const hiddenEdgeTypes = new Set();
const nodeTypeCounts = {};
const edgeTypeCounts = {};

// Expand depth setting (1–3)
let expandDepth = 1;
const EXPAND_NODE_CAP = 500;

// ── DOM refs ──────────────────────────────────────────────────────────────
const $loading     = document.getElementById('loading');
const $emptyState  = document.getElementById('empty-state');
const $stats       = document.getElementById('stats');
const $toast       = document.getElementById('toast');
const $panel       = document.getElementById('right-panel');
const $searchInput = document.getElementById('search-input');

// ── Toast ─────────────────────────────────────────────────────────────────
let toastTimer;
function showToast(msg, duration = 2500) {
  $toast.textContent = msg;
  $toast.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => $toast.classList.remove('show'), duration);
}

// ── Loading ───────────────────────────────────────────────────────────────
let loadingCount = 0;
function startLoading() { loadingCount++; $loading.classList.remove('hidden'); $loading.classList.add('flex'); }
function stopLoading()  { if (--loadingCount <= 0) { loadingCount = 0; $loading.classList.add('hidden'); $loading.classList.remove('flex'); } }

// ── Stats ─────────────────────────────────────────────────────────────────
function updateStats() {
  const visNodes = graph.filterNodes((n, d) => !d.hidden).length;
  const visEdges = graph.filterEdges((e, d) => !d.hidden).length;
  const total = graph.order, totalE = graph.size;
  let txt = `${total} node${total !== 1 ? 's' : ''} · ${totalE} edge${totalE !== 1 ? 's' : ''}`;
  if (visNodes < total || visEdges < totalE) txt += ` (${visNodes} · ${visEdges} visible)`;
  $stats.textContent = txt;
  $emptyState.style.display = total === 0 ? 'flex' : 'none';
}

// ── API helpers ───────────────────────────────────────────────────────────
async function api(path, body) {
  const res = await fetch('/proxy' + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`API ${path} failed: ${res.status}`);
  return res.json();
}

// ── Filter system ─────────────────────────────────────────────────────────
// Visibility toggle — called from click handlers on HTMX-rendered filter items.
// After toggle, we re-request the filter list from the server with updated state.

// syncHiddenInputs — writes current hidden-type state into the hidden <input>
// elements that are included via hx-include on each HTMX refresh call.
// Must be called before every htmx.trigger(document.body, 'refreshFilters').
function syncHiddenInputs() {
  const hn = document.getElementById('hidden-node-types');
  const he = document.getElementById('hidden-edge-types');
  const sf = document.getElementById('selected-type-filter');
  if (hn) hn.value = [...hiddenNodeTypes].join(',');
  if (he) he.value = [...hiddenEdgeTypes].join(',');
  if (sf) {
    // Pass the node type of the selected node (for relationship filtering)
    const selType = selectedNode ? ((nodeData[selectedNode] || {}).type || '') : '';
    sf.value = selType;
  }
}

function applyFilters() {
  graph.forEachNode((node) => {
    const type = (nodeData[node] || {}).type || 'unknown';
    graph.setNodeAttribute(node, 'hidden', hiddenNodeTypes.has(type));
  });
  graph.forEachEdge((edge) => {
    const et = (edgeData[edge] || {}).type || '';
    const [src, dst] = graph.extremities(edge);
    const st = (nodeData[src] || {}).type || 'unknown';
    const dt = (nodeData[dst] || {}).type || 'unknown';
    graph.setEdgeAttribute(edge, 'hidden',
      hiddenEdgeTypes.has(et) || hiddenNodeTypes.has(st) || hiddenNodeTypes.has(dt));
  });
  if (sigmaInstance) sigmaInstance.refresh();
  updateStats();
}

// Delegate click events on HTMX-rendered filter items
document.addEventListener('click', (e) => {
  // Node type load — clicking the label span loads nodes without toggling visibility
  const loadTarget = e.target.closest('[data-action="load"]');
  if (loadTarget) {
    e.stopPropagation(); // prevent bubbling up to toggle-vis handler on parent row
    const type = loadTarget.dataset.type || loadTarget.closest('[data-type]')?.dataset.type;
    if (type) loadNodesByType(type);
    return;
  }

  // Node type visibility toggle
  const visToggle = e.target.closest('[data-action="toggle-vis"]');
  if (visToggle) {
    e.stopPropagation();
    const type = visToggle.dataset.type || visToggle.closest('[data-type]')?.dataset.type;
    if (type) {
      if (hiddenNodeTypes.has(type)) hiddenNodeTypes.delete(type); else hiddenNodeTypes.add(type);
      applyFilters();
      // Re-render filter lists via HTMX
      syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
    }
    return;
  }

  // Edge type visibility toggle
  const edgeToggle = e.target.closest('[data-action="toggle-edge-vis"]');
  if (edgeToggle) {
    const type = edgeToggle.dataset.type;
    if (type) {
      if (hiddenEdgeTypes.has(type)) hiddenEdgeTypes.delete(type); else hiddenEdgeTypes.add(type);
      applyFilters();
      syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
    }
    return;
  }

  // Relation row click — navigate to node
  const navNode = e.target.closest('[data-action="navigate-node"]');
  if (navNode) {
    const nid = navNode.dataset.nodeId;
    if (nid && graph.hasNode(nid)) {
      selectNode(nid);
      sigmaInstance.getCamera().animate(
        { ...sigmaInstance.getNodeDisplayData(nid), ratio: 0.5 },
        { duration: 400 }
      );
    }
    return;
  }
});

// Toggle-all buttons
// Reads type names from the rendered filter-item rows so the toggle works
// even for types that have no nodes loaded in the graph yet.
function updateToggleAllLabels() {
  const nodeTypes = [...document.querySelectorAll('#node-filter-list [data-type]')].map(el => el.dataset.type);
  const edgeTypes = [...document.querySelectorAll('#edge-filter-list [data-type]')].map(el => el.dataset.type);
  const nodeBtn = document.getElementById('nodes-toggle-all');
  const edgeBtn = document.getElementById('edges-toggle-all');
  if (nodeBtn) {
    const allHidden = nodeTypes.length > 0 && nodeTypes.every(t => hiddenNodeTypes.has(t));
    nodeBtn.textContent = allHidden ? 'Show all' : 'Hide all';
  }
  if (edgeBtn) {
    const allHidden = edgeTypes.length > 0 && edgeTypes.every(t => hiddenEdgeTypes.has(t));
    edgeBtn.textContent = allHidden ? 'Show all' : 'Hide all';
  }
}

document.getElementById('nodes-toggle-all')?.addEventListener('click', () => {
  const types = [...document.querySelectorAll('#node-filter-list [data-type]')].map(el => el.dataset.type);
  if (!types.length) return;
  const allHidden = types.every(t => hiddenNodeTypes.has(t));
  if (allHidden) { types.forEach(t => hiddenNodeTypes.delete(t)); } else { types.forEach(t => hiddenNodeTypes.add(t)); }
  applyFilters();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
});
document.getElementById('edges-toggle-all')?.addEventListener('click', () => {
  const types = [...document.querySelectorAll('#edge-filter-list [data-type]')].map(el => el.dataset.type);
  if (!types.length) return;
  const allHidden = types.every(t => hiddenEdgeTypes.has(t));
  if (allHidden) { types.forEach(t => hiddenEdgeTypes.delete(t)); } else { types.forEach(t => hiddenEdgeTypes.add(t)); }
  applyFilters();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
});

// ── Graph helpers ─────────────────────────────────────────────────────────
function registerNodeType(t) { nodeTypeCounts[t] = (nodeTypeCounts[t] || 0) + 1; }
function registerEdgeType(t) { edgeTypeCounts[t] = (edgeTypeCounts[t] || 0) + 1; }

function nodeSize(deg) { return Math.min(14 + Math.sqrt(deg) * 4, 48); }

function mergeNode(n, { forceVisible = false } = {}) {
  const id = n.canonical_id || n.entity_id || n.id;
  if (!id) return;
  const type = n.type || 'unknown';
  const color = typeColor(type);
  const props = n.properties || {};
  const label = props.name || props.title || n.key || (type + ' ' + String(id).slice(0, 6));
  if (!graph.hasNode(id)) {
    graph.addNode(id, {
      label, size: 14, color,
      x: Math.random() * 10 - 5, y: Math.random() * 10 - 5,
      nodeType: type, typeInitial: typeIcon(type),
      hidden: forceVisible ? false : hiddenNodeTypes.has(type),
    });
    nodeData[id] = n;
    registerNodeType(type);
  } else if (forceVisible) {
    graph.setNodeAttribute(id, 'hidden', false);
  }
}

function mergeObjectResponse(o, { forceVisible = false } = {}) {
  const id = o.canonical_id || o.entity_id || o.id;
  if (!id) return;
  const type = o.type || 'unknown';
  const color = typeColor(type);
  const props = o.properties || {};
  const label = props.name || props.title || o.key || (type + ' ' + String(id).slice(0, 6));
  if (!graph.hasNode(id)) {
    graph.addNode(id, {
      label, size: 14, color,
      x: Math.random() * 10 - 5, y: Math.random() * 10 - 5,
      nodeType: type, typeInitial: typeIcon(type),
      hidden: forceVisible ? false : hiddenNodeTypes.has(type),
    });
    nodeData[id] = { ...o, canonical_id: id, properties: props };
    registerNodeType(type);
  } else if (forceVisible) {
    graph.setNodeAttribute(id, 'hidden', false);
  }
}

function mergeEdge(e, { forceVisible = false } = {}) {
  const src = e.src_id, dst = e.dst_id;
  if (!src || !dst || !graph.hasNode(src) || !graph.hasNode(dst)) return;
  const key = `${src}__${dst}`;
  const edgeType = e.type || '';
  if (!graph.hasEdge(key)) {
    try {
      const srcColor = graph.getNodeAttribute(src, 'color') || '#8b949e';
      graph.addEdgeWithKey(key, src, dst, {
        label: edgeType, size: 1.5,
        color: srcColor + '55',
        hidden: forceVisible ? false : hiddenEdgeTypes.has(edgeType),
      });
      edgeData[key] = e;
      registerEdgeType(edgeType);
      graph.setNodeAttribute(src, 'size', nodeSize(graph.degree(src)));
      graph.setNodeAttribute(dst, 'size', nodeSize(graph.degree(dst)));
    } catch (_) {}
  } else if (forceVisible) {
    graph.setEdgeAttribute(key, 'hidden', false);
  }
}

function refreshNodeSizes() {
  graph.forEachNode((node) => {
    const deg = graph.degree(node);
    graph.setNodeAttribute(node, 'size', nodeSize(deg));
  });
}

function mergeExpandResponse(resp) {
  const v2c = {};
  (resp.nodes || []).forEach(n => {
    const cid = n.canonical_id || n.entity_id;
    const vid = n.id || n.version_id;
    if (cid && vid) v2c[vid] = cid;
    mergeNode(n, { forceVisible: true });
  });
  (resp.edges || []).forEach(e => {
    mergeEdge({ ...e, src_id: v2c[e.src_id] || e.src_id, dst_id: v2c[e.dst_id] || e.dst_id }, { forceVisible: true });
  });
  updateStats();
  if (sigmaInstance) sigmaInstance.refresh();
  // Tell HTMX to refresh filter lists
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
}

function mergeSearchResponse(resp) {
  (resp.primaryResults || []).forEach(item => { if (item.object) mergeObjectResponse(item.object); });
  Object.entries(resp.neighbors || {}).forEach(([pid, list]) => {
    (list || []).forEach(nb => {
      mergeObjectResponse(nb);
      const nid = nb.canonical_id || nb.entity_id || nb.id;
      if (nid && pid !== nid) mergeEdge({ src_id: pid, dst_id: nid, type: '' });
    });
  });
  updateStats();
  if (sigmaInstance) sigmaInstance.refresh();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
}

// ── Layout system ─────────────────────────────────────────────────────────
let activeLayout = 'fa2';
let fa2RafId = null;
let fa2Running = false;
let fa2FrameCount = 0;
const FA2_ITERS_PER_FRAME = 3;
const FA2_MAX_FRAMES = 200;

function stopFA2() {
  if (fa2RafId !== null) { cancelAnimationFrame(fa2RafId); fa2RafId = null; }
  fa2Running = false; fa2FrameCount = 0;
  const btn = document.getElementById('btn-layout');
  if (btn) btn.innerHTML = `⟳ <span id="layout-label">${layoutLabel(activeLayout)}</span>`;
}

function layoutLabel(key) {
  return { fa2: 'ForceAtlas2', circular: 'Circular', circlepack: 'Circle Pack', random: 'Random' }[key] || key;
}

function setActiveLayout(key) {
  activeLayout = key;
  document.getElementById('layout-label').textContent = layoutLabel(key);
  document.querySelectorAll('.layout-opt').forEach(el => {
    el.classList.toggle('active-layout', el.id === `layout-opt-${key}`);
  });
  document.activeElement?.blur();
}

function startFA2({ resetPositions = true } = {}) {
  if (graph.order === 0) return;
  stopFA2();
  if (resetPositions) circular.assign(graph, { scale: 5 });
  refreshNodeSizes();

  const settings = forceAtlas2.inferSettings(graph);
  settings.gravity = 1;
  settings.scalingRatio = 2;
  settings.barnesHutOptimize = graph.order > 100;
  settings.adjustSizes = false;

  fa2Running = true; fa2FrameCount = 0;
  const btn = document.getElementById('btn-layout');
  if (btn) btn.innerHTML = '◼ <span id="layout-label">Stop</span>';

  function frame() {
    if (!fa2Running) return;
    const positions = forceAtlas2(graph, { iterations: FA2_ITERS_PER_FRAME, settings });
    graph.updateEachNodeAttributes((node, attrs) => ({
      ...attrs, x: positions[node].x, y: positions[node].y,
    }));
    if (sigmaInstance) sigmaInstance.refresh();
    fa2FrameCount++;
    if (fa2FrameCount < FA2_MAX_FRAMES) {
      fa2RafId = requestAnimationFrame(frame);
    } else {
      stopFA2();
      if (sigmaInstance) sigmaInstance.getCamera().animatedReset({ duration: 500 });
    }
  }

  fa2RafId = requestAnimationFrame(frame);
  setTimeout(() => { if (sigmaInstance) sigmaInstance.getCamera().animatedReset({ duration: 600 }); }, 100);
}

function runLayout({ resetPositions = true } = {}) {
  if (graph.order === 0) return;
  stopFA2();

  if (activeLayout === 'fa2') {
    startFA2({ resetPositions });
    return;
  }

  refreshNodeSizes();

  if (activeLayout === 'circular') {
    circular.assign(graph, { scale: 5 });
  } else if (activeLayout === 'circlepack') {
    circlepack.assign(graph, { scale: 5 });
  } else if (activeLayout === 'random') {
    random.assign(graph, { scale: 5 });
    noverlap.assign(graph, { maxIterations: 100, settings: { ratio: 1.2, speed: 3 } });
  }

  if (sigmaInstance) {
    sigmaInstance.refresh();
    sigmaInstance.getCamera().animatedReset({ duration: 500 });
  }
}

// ── Custom node label renderer ────────────────────────────────────────────
function drawNodeIcon(context, data) {
  const { size, x, y, typeInitial, label } = data;
  const icon = typeInitial || (label || '?').charAt(0).toUpperCase();
  const isEmoji = [...icon].length > 1;
  const fontSize = isEmoji ? Math.max(6, Math.round(size * 0.75)) : Math.max(5, Math.round(size * 0.65));
  context.save();
  context.font = isEmoji
    ? `${fontSize}px sans-serif`
    : `700 ${fontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'center';
  context.textBaseline = 'middle';
  context.fillStyle = 'rgba(255,255,255,0.92)';
  context.fillText(icon, x, y);
  context.restore();
}

function drawNodeLabel(context, data, settings) {
  drawNodeIcon(context, data);

  // When the reducer sets a label (zoomed in or selected), draw the two-line chip
  const { size, x, y, color, label, typeInitial, nodeType } = data;
  if (!label) return;

  const icon = typeInitial || (label || '?').charAt(0).toUpperCase();
  const isEmoji = [...icon].length > 1;
  const typeStr = nodeType || '';

  const nameFontSize = 11;
  const typeFontSize = 9;
  const pad = 5;
  const dotR = 7;
  const iconFs = isEmoji ? 10 : 8;
  const gap = 5;
  const lineGap = 2;

  context.save();

  context.font = `500 ${nameFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  const nameW = context.measureText(label).width;
  context.font = `400 ${typeFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  const typeW = context.measureText(typeStr).width;

  const textW = Math.max(nameW, typeW);
  const chipW = pad + dotR * 2 + gap + textW + pad;
  const chipH = typeFontSize + lineGap + nameFontSize + 8;
  const chipX = x + size + 6;
  const chipY = y - chipH / 2;

  context.fillStyle = 'rgba(13,17,23,0.88)';
  context.strokeStyle = 'rgba(48,54,61,0.65)';
  context.lineWidth = 0.8;
  context.beginPath();
  context.roundRect(chipX, chipY, chipW, chipH, 6);
  context.fill();
  context.stroke();

  const dotCX = chipX + pad + dotR;
  const dotCY = y;
  context.beginPath();
  context.arc(dotCX, dotCY, dotR, 0, Math.PI * 2);
  context.fillStyle = color;
  context.fill();

  context.font = isEmoji ? `${iconFs}px sans-serif` : `700 ${iconFs}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'center';
  context.textBaseline = 'middle';
  context.fillStyle = 'rgba(255,255,255,0.92)';
  context.fillText(icon, dotCX, dotCY);

  const textX = chipX + pad + dotR * 2 + gap;
  const typeY = chipY + 4 + typeFontSize / 2;
  context.font = `400 ${typeFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'left';
  context.textBaseline = 'middle';
  context.fillStyle = '#8b949e';
  context.fillText(typeStr, textX, typeY);

  const nameY = typeY + typeFontSize / 2 + lineGap + nameFontSize / 2;
  context.font = `500 ${nameFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'left';
  context.textBaseline = 'middle';
  context.fillStyle = '#e6edf3';
  context.fillText(label, textX, nameY);

  context.restore();
}

function drawNodeHoverLabel(context, data, settings) {
  drawNodeIcon(context, data);

  const { size, x, y, color, label, typeInitial, nodeType } = data;
  if (!label) return;

  const icon = typeInitial || (label || '?').charAt(0).toUpperCase();
  const isEmoji = [...icon].length > 1;
  const typeStr = nodeType || '';

  // Two-line chip dimensions
  const nameFontSize = 11;
  const typeFontSize = 9;
  const pad = 5;
  const dotR = 7;
  const iconFs = isEmoji ? 10 : 8;
  const gap = 5;
  const lineGap = 2;

  context.save();

  // Measure both lines
  context.font = `500 ${nameFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  const nameW = context.measureText(label).width;
  context.font = `400 ${typeFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  const typeW = context.measureText(typeStr).width;

  const textW = Math.max(nameW, typeW);
  const chipW = pad + dotR * 2 + gap + textW + pad;
  const chipH = typeFontSize + lineGap + nameFontSize + 8; // top pad + type + gap + name + bottom pad
  const chipX = x + size + 6;
  const chipY = y - chipH / 2;

  // Background pill
  context.fillStyle = 'rgba(13,17,23,0.90)';
  context.strokeStyle = 'rgba(48,54,61,0.75)';
  context.lineWidth = 0.8;
  context.beginPath();
  context.roundRect(chipX, chipY, chipW, chipH, 6);
  context.fill();
  context.stroke();

  // Colored dot with icon (vertically centered in chip)
  const dotCX = chipX + pad + dotR;
  const dotCY = y;
  context.beginPath();
  context.arc(dotCX, dotCY, dotR, 0, Math.PI * 2);
  context.fillStyle = color;
  context.fill();

  context.font = isEmoji ? `${iconFs}px sans-serif` : `700 ${iconFs}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'center';
  context.textBaseline = 'middle';
  context.fillStyle = 'rgba(255,255,255,0.92)';
  context.fillText(icon, dotCX, dotCY);

  // Type label (line 1 — dimmed, smaller)
  const textX = chipX + pad + dotR * 2 + gap;
  const typeY = chipY + 4 + typeFontSize / 2;
  context.font = `400 ${typeFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'left';
  context.textBaseline = 'middle';
  context.fillStyle = '#8b949e';
  context.fillText(typeStr, textX, typeY);

  // Entity name (line 2 — normal)
  const nameY = typeY + typeFontSize / 2 + lineGap + nameFontSize / 2;
  context.font = `500 ${nameFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'left';
  context.textBaseline = 'middle';
  context.fillStyle = '#e6edf3';
  context.fillText(label, textX, nameY);

  context.restore();
}

// ── Hover card ────────────────────────────────────────────────────────────
const $hoverCard  = document.getElementById('node-hover-card');
const $nhcDot     = document.getElementById('nhc-dot');
const $nhcType    = document.getElementById('nhc-type');
const $nhcName    = document.getElementById('nhc-name');
const $nhcProps   = document.getElementById('nhc-props');
const $nhcEdges   = document.getElementById('nhc-edges');
let hoverHideTimer = null;

// Top-5 property keys to skip (already shown in header or rarely useful)
const HC_SKIP_KEYS = new Set(['name', 'title', 'id', 'canonical_id', 'entity_id', 'type']);
const HC_MAX_PROPS = 4;

function populateHoverCard(nodeId, color, typeName, label, inGraphProps) {
  $nhcDot.style.background = color;
  $nhcType.textContent = typeName || '';
  $nhcType.style.color = color;
  $nhcName.textContent = label || nodeId;

  // Properties
  $nhcProps.innerHTML = '';
  const propsToShow = [];
  if (inGraphProps) {
    for (const [k, v] of Object.entries(inGraphProps)) {
      if (HC_SKIP_KEYS.has(k)) continue;
      if (v === null || v === undefined || v === '') continue;
      const val = typeof v === 'object' ? JSON.stringify(v) : String(v);
      propsToShow.push({ k, val });
      if (propsToShow.length >= HC_MAX_PROPS) break;
    }
  }
  for (const { k, val } of propsToShow) {
    const row = document.createElement('div');
    row.className = 'flex gap-1.5 items-baseline';
    row.innerHTML = `<span style="color:#8b949e;font-size:10px;white-space:nowrap">${escHtml(k)}</span>`
      + `<span style="color:#e6edf3;font-size:11px;font-family:monospace;word-break:break-all;flex:1">${escHtml(val.slice(0, 80))}</span>`;
    $nhcProps.appendChild(row);
  }

  // Edge count in graph
  const deg = (sigmaInstance && graph.hasNode(nodeId)) ? graph.degree(nodeId) : null;
  if (deg !== null) {
    $nhcEdges.textContent = `${deg} connection${deg !== 1 ? 's' : ''} in graph`;
    $nhcEdges.style.display = '';
  } else {
    $nhcEdges.style.display = 'none';
  }
}

function showHoverCard(nodeId, screenX, screenY, color, typeName, label, inGraphProps) {
  clearTimeout(hoverHideTimer);
  populateHoverCard(nodeId, color, typeName, label, inGraphProps);

  // Position: prefer right of cursor, flip left if near right edge
  const margin = 12;
  const cardW = 220;
  const vw = window.innerWidth;
  const vh = window.innerHeight;

  let left = screenX + margin;
  if (left + cardW > vw - 8) left = screenX - cardW - margin;

  $hoverCard.style.left = `${left}px`;
  $hoverCard.style.top  = `${Math.min(screenY - 8, vh - 200)}px`;
  $hoverCard.classList.remove('hidden');
}

function hideHoverCard(delay = 0) {
  if (delay > 0) {
    hoverHideTimer = setTimeout(() => $hoverCard.classList.add('hidden'), delay);
  } else {
    clearTimeout(hoverHideTimer);
    $hoverCard.classList.add('hidden');
  }
}

function escHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// ── Sigma init ────────────────────────────────────────────────────────────
function initSigma() {
  sigmaInstance = new Sigma(graph, document.getElementById('sigma-container'), {
    renderEdgeLabels: true,
    defaultEdgeType: 'arrow',
    labelFont: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    labelSize: 11, labelWeight: '500',
    labelColor: { color: '#e6edf3' },
    labelRenderedSizeThreshold: 6,
    edgeLabelSize: 9,
    edgeLabelColor: { color: '#8b949e' },
    minCameraRatio: 0.05, maxCameraRatio: 20,
    defaultDrawNodeLabel: drawNodeLabel,
    defaultDrawNodeHover: drawNodeHoverLabel,

    nodeReducer(node, data) {
      const res = { ...data };
      if (data.hidden) return { ...res, hidden: true };
      const deg = graph.degree(node);
      res.size = nodeSize(deg);
      const cameraRatio = sigmaInstance?.getCamera().getState().ratio ?? 1;
      const zoomedIn = cameraRatio < 0.5;
      if (selectedNode !== null) {
        if (!graph.hasNode(selectedNode)) {
          // Selected node was removed — treat as no selection
          res.label = zoomedIn ? data.label : '';
        } else if (node === selectedNode) {
          res.highlighted = true; res.size *= 1.5; res.zIndex = 2;
          // label always shown for selected node (already set by data.label)
        } else if (graph.neighbors(selectedNode).includes(node)) {
          res.color = data.color;
          res.label = data.label; // always show neighbor labels when selected
        } else {
          res.color = '#1c2128'; res.label = ''; res.opacity = 0.3;
        }
      } else {
        res.label = zoomedIn ? data.label : '';
      }
      return res;
    },

    edgeReducer(edge, data) {
      const res = { ...data };
      const [src, dst] = graph.extremities(edge);
      if (data.hidden || graph.getNodeAttribute(src, 'hidden') || graph.getNodeAttribute(dst, 'hidden'))
        return { ...res, hidden: true };
      if (selectedNode !== null && graph.hasNode(selectedNode)) {
        if (src === selectedNode || dst === selectedNode) {
          res.color = graph.getNodeAttribute(src, 'color') + 'cc'; res.size = 2.5; res.zIndex = 1;
        } else {
          res.color = 'rgba(48,54,61,0.15)'; res.size = 0.8;
        }
      }
      return res;
    },
  });

  sigmaInstance.on('clickNode', ({ node }) => { selectNode(node); });
  sigmaInstance.on('doubleClickNode', ({ node }) => { expandNode(node); });
  sigmaInstance.on('rightClickNode', ({ node, event }) => {
    event.preventDefault();
    selectNode(node);
    showContextMenu(node, event.clientX, event.clientY);
  });
  sigmaInstance.on('clickStage', () => deselectNode());
  // Re-render on camera move so semantic zoom labels update
  sigmaInstance.getCamera().on('updated', () => sigmaInstance.refresh());

  enableNodeDrag();
}

// ── Node selection ────────────────────────────────────────────────────────
function selectNode(id) {
  if (!graph.hasNode(id)) return;
  if (id !== selectedNode) {
    if (expandedNode) collapseExpanded();
    if (focusActive) { focusActive = false; applyFilters(); }
  }
  selectedNode = id;
  sigmaInstance.refresh();

  // Open right panel and trigger HTMX to load detail content
  const d = nodeData[id] || {};
  const entityId = d.canonical_id || d.entity_id || id;
  document.getElementById('selected-node-id').value = entityId;

  // Use htmx.ajax to directly fetch node detail into the panel body
  htmx.ajax('GET', `/htmx/node-detail?nodeId=${encodeURIComponent(entityId)}`, {
    target: '#panel-body',
    swap: 'innerHTML',
  });

  $panel.classList.add('open');
  setTimeout(() => sigmaInstance?.resize(), 210);

  updateFocusBtn();
  updateExpandBtn();
}

function deselectNode() {
  if (expandedNode) collapseExpanded();
  if (focusActive) { focusActive = false; applyFilters(); }
  selectedNode = null;
  sigmaInstance.refresh();
  $panel.classList.remove('open');
  setTimeout(() => sigmaInstance?.resize(), 210);
  updateFocusBtn();
  updateExpandBtn();
}

// ── Panel actions ─────────────────────────────────────────────────────────
document.getElementById('panel-close').addEventListener('click', deselectNode);

// Depth selector buttons
document.addEventListener('click', (e) => {
  const depthBtn = e.target.closest('.depth-btn');
  if (!depthBtn) return;
  expandDepth = parseInt(depthBtn.id.replace('depth-', ''), 10);
  document.querySelectorAll('.depth-btn').forEach(b => {
    const active = b.id === depthBtn.id;
    b.style.background = active ? 'var(--color-gh-accent, #58a6ff)' : '';
    b.style.color = active ? '#fff' : '';
    b.classList.toggle('text-gh-muted', !active);
  });
});

document.getElementById('btn-expand').addEventListener('click', async () => {
  if (!selectedNode) return;
  if (expandedNode === selectedNode) {
    collapseExpanded();
  } else {
    await expandNode(selectedNode);
  }
});

document.getElementById('btn-copy-id').addEventListener('click', () => {
  if (!selectedNode) return;
  const id = (nodeData[selectedNode] || {}).id || (nodeData[selectedNode] || {}).canonical_id || selectedNode;
  navigator.clipboard.writeText(id).then(() => showToast('Copied ID to clipboard'));
});

let focusActive = false;

function updateFocusBtn() {
  const btn = document.getElementById('btn-focus');
  if (focusActive) {
    btn.textContent = 'Exit focus';
    btn.classList.add('border-gh-accent', 'text-gh-accent');
    btn.classList.remove('text-gh-text');
  } else {
    btn.textContent = 'Focus subgraph';
    btn.classList.remove('border-gh-accent', 'text-gh-accent');
    btn.classList.add('text-gh-text');
  }
}

document.getElementById('btn-focus').addEventListener('click', () => {
  if (!selectedNode) return;
  if (focusActive) {
    focusActive = false;
    applyFilters();
    updateFocusBtn();
    showToast('Focus cleared');
  } else {
    focusActive = true;
    const neighbors = new Set([...graph.neighbors(selectedNode), selectedNode]);
    graph.forEachNode(n => graph.setNodeAttribute(n, 'hidden', !neighbors.has(n)));
    graph.forEachEdge((e) => {
      const [s, d] = graph.extremities(e);
      graph.setEdgeAttribute(e, 'hidden', !neighbors.has(s) || !neighbors.has(d));
    });
    sigmaInstance.refresh(); updateStats();
    updateFocusBtn();
    showToast('Showing subgraph — click again to exit');
  }
});

document.getElementById('btn-layout').addEventListener('click', () => {
  if (fa2Running) stopFA2(); else runLayout({ resetPositions: false });
});

// Layout dropdown option handlers
document.getElementById('layout-opt-fa2').addEventListener('click', () => { setActiveLayout('fa2'); runLayout(); });
document.getElementById('layout-opt-circular').addEventListener('click', () => { setActiveLayout('circular'); runLayout(); });
document.getElementById('layout-opt-circlepack').addEventListener('click', () => { setActiveLayout('circlepack'); runLayout(); });
document.getElementById('layout-opt-random').addEventListener('click', () => { setActiveLayout('random'); runLayout(); });

document.getElementById('btn-fit').addEventListener('click', () => {
  if (sigmaInstance) sigmaInstance.getCamera().animatedReset({ duration: 500 });
});

document.getElementById('btn-zoom-in').addEventListener('click', () => {
  if (!sigmaInstance) return;
  const cam = sigmaInstance.getCamera();
  cam.animate({ ratio: cam.ratio / 1.5 }, { duration: 200 });
});

document.getElementById('btn-zoom-out').addEventListener('click', () => {
  if (!sigmaInstance) return;
  const cam = sigmaInstance.getCamera();
  cam.animate({ ratio: cam.ratio * 1.5 }, { duration: 200 });
});

document.getElementById('btn-clear').addEventListener('click', () => {
  stopFA2();
  graph.clear();
  nodeData = {}; edgeData = {};
  selectedNode = null;
  expandedNode = null; expandedNodeIds.clear(); expandedEdgeKeys.clear();
  focusActive = false;
  $panel.classList.remove('open');
  hiddenNodeTypes.clear(); hiddenEdgeTypes.clear();
  Object.keys(nodeTypeCounts).forEach(k => delete nodeTypeCounts[k]);
  Object.keys(edgeTypeCounts).forEach(k => delete edgeTypeCounts[k]);
  updateStats();
  updateExpandBtn(); updateFocusBtn();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
});

// ── Expand (toggle) ───────────────────────────────────────────────────────
let expandedNode = null;
let expandedNodeIds = new Set();
let expandedEdgeKeys = new Set();

function updateExpandBtn() {
  const btn = document.getElementById('btn-expand');
  if (expandedNode && expandedNode === selectedNode) {
    btn.textContent = 'Collapse neighbors';
    btn.classList.add('border-gh-accent', 'text-gh-accent');
    btn.classList.remove('btn-primary');
  } else {
    btn.textContent = 'Expand neighbors';
    btn.classList.remove('border-gh-accent', 'text-gh-accent');
    btn.classList.add('btn-primary');
  }
}

function collapseExpanded() {
  if (!expandedNode) return;
  for (const key of expandedEdgeKeys) {
    if (graph.hasEdge(key)) {
      const et = (edgeData[key] || {}).type || '';
      graph.dropEdge(key);
      delete edgeData[key];
      if (edgeTypeCounts[et] > 0) edgeTypeCounts[et]--;
    }
  }
  for (const nid of expandedNodeIds) {
    if (graph.hasNode(nid) && graph.degree(nid) === 0) {
      const type = (nodeData[nid] || {}).type || 'unknown';
      graph.dropNode(nid);
      delete nodeData[nid];
      if (nodeTypeCounts[type] > 0) nodeTypeCounts[type]--;
    }
  }
  expandedNode = null;
  expandedNodeIds.clear();
  expandedEdgeKeys.clear();
  updateStats();
  if (sigmaInstance) sigmaInstance.refresh();
  updateExpandBtn();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
}

async function expandNode(nodeId) {
  if (expandedNode && expandedNode !== nodeId) collapseExpanded();

  const d = nodeData[nodeId] || {};
  const id = d.canonical_id || d.entity_id || nodeId;
  startLoading();
  try {
    const resp = await api('/api/graph/expand', { root_ids: [id], max_depth: expandDepth, max_nodes: EXPAND_NODE_CAP });

    const nodesBefore = new Set(graph.nodes());
    const edgesBefore = new Set(graph.edges());

    const prevOrder = graph.order;
    mergeExpandResponse(resp);

    expandedNodeIds.clear();
    expandedEdgeKeys.clear();
    graph.forEachNode(n => { if (!nodesBefore.has(n) && n !== nodeId) expandedNodeIds.add(n); });
    graph.forEachEdge(e => { if (!edgesBefore.has(e)) expandedEdgeKeys.add(e); });

    expandedNode = nodeId;
    const added = graph.order - prevOrder;
    runLayout({ resetPositions: prevOrder === 0 });
    showToast(added > 0 ? `+${added} node${added !== 1 ? 's' : ''} added` : 'No new neighbors found');
    updateExpandBtn();
  } catch (e) { showToast('Expand failed: ' + e.message, 3500); }
  finally { stopLoading(); }
}

// ── Load nodes by type ────────────────────────────────────────────────────
async function loadNodesByType(type) {
  startLoading();
  try {
    const params = new URLSearchParams({ type, limit: '100' });
    const res = await fetch(`/proxy/api/graph/objects/search?${params}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    const objects = data.data || data.objects || data.items || data.results || [];
    if (!objects.length) { showToast(`No ${type} nodes found`); return; }
    const prevOrder = graph.order;
    objects.forEach(o => mergeObjectResponse(o));
    const added = graph.order - prevOrder;
    updateStats();
    if (sigmaInstance) sigmaInstance.refresh();
    runLayout({ resetPositions: prevOrder === 0 });
    showToast(`+${added} ${type} node${added !== 1 ? 's' : ''} loaded`);
    syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
  } catch (e) { showToast('Load failed: ' + e.message, 3500); }
  finally { stopLoading(); }
}

// ── Node drag ─────────────────────────────────────────────────────────────
// Implemented via Sigma's mousedown/mousemove/mouseup events on the container.
let dragState = null; // { node, startX, startY }

function enableNodeDrag() {
  const container = document.getElementById('sigma-container');

  container.addEventListener('mousedown', (e) => {
    if (!sigmaInstance || e.button !== 0) return;
    const { x, y } = sigmaInstance.viewportToGraph({ x: e.clientX - container.getBoundingClientRect().left, y: e.clientY - container.getBoundingClientRect().top });
    // Find nearest node within ~20px
    let nearest = null, nearestDist = Infinity;
    graph.forEachNode((node, attrs) => {
      if (attrs.hidden) return;
      const nx = attrs.x, ny = attrs.y;
      const display = sigmaInstance.graphToViewport({ x: nx, y: ny });
      const rect = container.getBoundingClientRect();
      const dx = display.x - (e.clientX - rect.left);
      const dy = display.y - (e.clientY - rect.top);
      const dist = Math.sqrt(dx * dx + dy * dy);
      if (dist < nearestDist && dist < 30) { nearest = node; nearestDist = dist; }
    });
    if (nearest) {
      dragState = { node: nearest };
      sigmaInstance.getCamera().disable();
      e.stopPropagation();
    }
  });

  container.addEventListener('mousemove', (e) => {
    if (!dragState || !sigmaInstance) return;
    const rect = container.getBoundingClientRect();
    const { x, y } = sigmaInstance.viewportToGraph({ x: e.clientX - rect.left, y: e.clientY - rect.top });
    graph.setNodeAttribute(dragState.node, 'x', x);
    graph.setNodeAttribute(dragState.node, 'y', y);
    sigmaInstance.refresh();
    e.stopPropagation();
  });

  const endDrag = () => {
    if (dragState) {
      dragState = null;
      if (sigmaInstance) sigmaInstance.getCamera().enable();
    }
  };
  container.addEventListener('mouseup', endDrag);
  document.addEventListener('mouseup', endDrag);
}


const $ctxMenu = document.getElementById('ctx-menu');
let ctxMenuNode = null;

function showContextMenu(node, x, y) {
  ctxMenuNode = node;
  const d = nodeData[node] || {};
  const type = d.type || 'unknown';
  document.getElementById('ctx-hide-type').textContent = `Hide all "${type}"`;
  $ctxMenu.style.left = `${x}px`;
  $ctxMenu.style.top  = `${y}px`;
  $ctxMenu.classList.remove('hidden');
}

function hideContextMenu() {
  $ctxMenu.classList.add('hidden');
  ctxMenuNode = null;
}

document.getElementById('ctx-expand').addEventListener('click', () => {
  if (ctxMenuNode) { selectNode(ctxMenuNode); expandNode(ctxMenuNode); }
  hideContextMenu();
});

document.getElementById('ctx-copy-id').addEventListener('click', () => {
  if (ctxMenuNode) {
    const d = nodeData[ctxMenuNode] || {};
    const id = d.id || d.canonical_id || ctxMenuNode;
    navigator.clipboard.writeText(id).then(() => showToast('Copied ID to clipboard'));
  }
  hideContextMenu();
});

document.getElementById('ctx-hide-type').addEventListener('click', () => {
  if (ctxMenuNode) {
    const type = (nodeData[ctxMenuNode] || {}).type || 'unknown';
    hiddenNodeTypes.add(type);
    applyFilters();
    syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
    showToast(`Hidden type: ${type}`);
  }
  hideContextMenu();
});

// Dismiss context menu on any click/escape outside
document.addEventListener('click', (e) => {
  if (!$ctxMenu.contains(e.target)) hideContextMenu();
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    if (!$ctxMenu.classList.contains('hidden')) {
      hideContextMenu();
    } else {
      deselectNode();
    }
  }
});


async function doSearch() {
  const q = $searchInput.value.trim();
  if (!q) return;
  startLoading();
  try {
    const resp = await api('/api/graph/search-with-neighbors', {
      query: q, limit: 20, includeNeighbors: true, maxNeighbors: 5,
    });
    const prevOrder = graph.order;
    mergeSearchResponse(resp);
    runLayout({ resetPositions: prevOrder === 0 });
    if (!(resp.primaryResults?.length > 0)) showToast('No results found');
  } catch (e) { showToast('Search failed: ' + e.message, 3500); }
  finally { stopLoading(); }
}

document.getElementById('search-btn').addEventListener('click', doSearch);
$searchInput.addEventListener('keydown', e => { if (e.key === 'Enter') doSearch(); });

// ── Load all nodes ────────────────────────────────────────────────────────
async function loadAllNodes() {
  const types = [...document.querySelectorAll('#node-filter-list [data-type]')].map(el => el.dataset.type);
  if (!types.length) {
    showToast('No node types found — load schema first');
    return;
  }
  const confirmed = window.confirm(
    `This will load all nodes across ${types.length} type(s).\n\nThis may be slow or overwhelming for large graphs.\n\nContinue?`
  );
  if (!confirmed) return;

  startLoading();
  let totalAdded = 0;
  try {
    for (const type of types) {
      const params = new URLSearchParams({ type, limit: '1000' });
      const res = await fetch(`/proxy/api/graph/objects/search?${params}`);
      if (!res.ok) continue;
      const data = await res.json();
      const objects = data.data || data.objects || data.items || data.results || [];
      const prevOrder = graph.order;
      objects.forEach(o => mergeObjectResponse(o));
      totalAdded += graph.order - prevOrder;
    }
    updateStats();
    if (sigmaInstance) sigmaInstance.refresh();
    runLayout({ resetPositions: true });
    showToast(`+${totalAdded} nodes loaded across ${types.length} types`);
    syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
  } catch (e) { showToast('Load all failed: ' + e.message, 3500); }
  finally { stopLoading(); }
}

document.getElementById('btn-load-all').addEventListener('click', loadAllNodes);

// ── Boot ──────────────────────────────────────────────────────────────────
// Update toggle-all button labels whenever the filter lists are re-rendered.
document.addEventListener('htmx:after:swap', (e) => {
  const id = e.target?.id;
  if (id === 'node-filter-list' || id === 'edge-filter-list') {
    updateToggleAllLabels();
  }
});

initSigma();
updateStats();
