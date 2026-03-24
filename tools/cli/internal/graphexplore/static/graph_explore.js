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
let currentBranchID = window.__BRANCH_ID || '';

// ── Palette ───────────────────────────────────────────────────────────────
const PALETTE = [
  '#58a6ff','#bc8cff','#3fb950','#d29922','#f78166',
  '#79c0ff','#ffa657','#ff7b72','#56d364','#e3b341',
  '#a5d6ff','#d2a8ff','#7ee787','#f0883e','#ff9492',
];
const typeColorCache = {};
let paletteIdx = 0;

// Pre-seed from server-injected type colors (available before HTMX loads filter lists)
if (window.__TYPE_COLORS) Object.assign(typeColorCache, window.__TYPE_COLORS);

function typeColor(type) {
  if (!typeColorCache[type]) typeColorCache[type] = PALETTE[paletteIdx++ % PALETTE.length];
  return typeColorCache[type];
}

// Sync type colors from server-rendered DOM (data-color attributes on filter items).
// Must be called after initial load and after every HTMX swap of the filter lists.
// Also recolors any existing graph nodes whose color doesn't match.
function syncTypeColorsFromDOM() {
  let changed = false;
  document.querySelectorAll('#node-filter-list [data-type][data-color]').forEach(el => {
    const t = el.dataset.type, c = el.dataset.color;
    if (t && c && typeColorCache[t] !== c) { typeColorCache[t] = c; changed = true; }
  });
  document.querySelectorAll('#edge-filter-list [data-type][data-color]').forEach(el => {
    const t = el.dataset.type, c = el.dataset.color;
    if (t && c && typeColorCache[t] !== c) { typeColorCache[t] = c; changed = true; }
  });
  // Recolor existing graph nodes if colors changed
  if (changed && graph.order > 0) {
    graph.forEachNode((id, attrs) => {
      const expected = typeColorCache[attrs.nodeType];
      if (expected && attrs.color !== expected) graph.setNodeAttribute(id, 'color', expected);
    });
    if (sigmaInstance) sigmaInstance.refresh();
  }
}
// Seed color cache on page load (may be empty if HTMX hasn't loaded filter lists yet)
syncTypeColorsFromDOM();
// Re-sync after HTMX swaps the filter lists
document.body.addEventListener('htmx:afterSettle', (e) => {
  const tgt = e.detail?.target;
  if (tgt && (tgt.id === 'node-filter-list' || tgt.id === 'edge-filter-list')) {
    syncTypeColorsFromDOM();
  }
});

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

// ── Schema mode state ─────────────────────────────────────────────────────
let isSchemaMode = false;
let preSchemaGraphState = null;
let schemaData = null; // { types: [...], rels: [...] }
let selectedSchemaType = null;

// ── Diff mode state ───────────────────────────────────────────────────────
let isDiffMode = false;
// Map<canonicalId, 'added'|'fast_forward'|'conflict'>
let diffStatusMap = new Map();
// Snapshot of nodeData/edgeData/graph state before entering diff mode
let preDiffGraphState = null;

const DIFF_COLORS = {
  added:        '#22c55e',  // green
  fast_forward: '#eab308',  // yellow
  conflict:     '#f97316',  // orange
};

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
// appendBranch adds branch_id to a URL if a branch is active.
function appendBranch(url) {
  if (!currentBranchID) return url;
  const sep = url.includes('?') ? '&' : '?';
  return url + sep + 'branch_id=' + encodeURIComponent(currentBranchID);
}

async function api(path, body) {
  const res = await fetch(appendBranch('/proxy' + path), {
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

  // Diff status badge (shown below the chip in diff mode)
  if (isDiffMode) {
    const diffStatus = diffStatusMap.get(data.id);
    if (diffStatus) {
      const badgeColor = DIFF_COLORS[diffStatus] || DIFF_COLORS.fast_forward;
      const badgeLabel = diffStatus === 'added' ? 'new' : 'updated';
      const badgeFontSize = 8;
      context.font = `600 ${badgeFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
      const badgeW = context.measureText(badgeLabel).width + 8;
      const badgeH = badgeFontSize + 6;
      const badgeX = chipX;
      const badgeY = chipY + chipH + 2;
      context.fillStyle = badgeColor + '33';
      context.strokeStyle = badgeColor;
      context.lineWidth = 0.8;
      context.beginPath();
      context.roundRect(badgeX, badgeY, badgeW, badgeH, 3);
      context.fill();
      context.stroke();
      context.fillStyle = badgeColor;
      context.textAlign = 'left';
      context.textBaseline = 'middle';
      context.fillText(badgeLabel, badgeX + 4, badgeY + badgeH / 2);
    }
  }

  // Schema mode: show "N props" pill below the chip
  if (isSchemaMode && data.propCount !== undefined) {
    const pillLabel = `${data.propCount} props`;
    const pillFontSize = 8;
    context.font = `500 ${pillFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
    const pillW = context.measureText(pillLabel).width + 10;
    const pillH = pillFontSize + 6;
    const pillX = chipX;
    const pillY = chipY + chipH + 2;
    context.fillStyle = 'rgba(88,166,255,0.12)';
    context.strokeStyle = 'rgba(88,166,255,0.4)';
    context.lineWidth = 0.8;
    context.beginPath();
    context.roundRect(pillX, pillY, pillW, pillH, 3);
    context.fill();
    context.stroke();
    context.fillStyle = '#8b949e';
    context.textAlign = 'left';
    context.textBaseline = 'middle';
    context.fillText(pillLabel, pillX + 5, pillY + pillH / 2);
  }

  context.restore();
}

function drawNodeHoverLabel(context, data, settings) {
  // Only draw the icon inside the node circle.
  // The rich hover card is an HTML overlay, positioned by enterNode/leaveNode events.
  drawNodeIcon(context, data);
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

      // Schema mode — always show labels, highlight selected + neighbors
      if (isSchemaMode) {
        res.size = 22;
        res.label = data.label;
        res.borderSize = 3;
        res.borderColor = data.color;
        if (selectedNode !== null && graph.hasNode(selectedNode)) {
          if (node === selectedNode) {
            res.highlighted = true; res.size = 28; res.zIndex = 2;
          } else if (graph.neighbors(selectedNode).includes(node)) {
            // neighbor — keep visible
          } else {
            res.color = data.color + '44'; res.borderColor = data.color + '44';
          }
        }
        return res;
      }

      // In diff mode, apply diff status color (already set on the node attr)
      if (isDiffMode) {
        const diffStatus = graph.getNodeAttribute(node, 'diffStatus');
        if (diffStatus && DIFF_COLORS[diffStatus]) {
          res.color = DIFF_COLORS[diffStatus];
          res.borderColor = DIFF_COLORS[diffStatus];
          res.borderSize = 3;
        }
        res.label = data.label; // always show labels in diff mode
        return res;
      }

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

      // Schema mode — always show edge labels, dim unconnected
      if (isSchemaMode) {
        res.forceLabel = true;
        if (selectedNode !== null && graph.hasNode(selectedNode)) {
          if (src === selectedNode || dst === selectedNode) {
            res.size = 3; res.zIndex = 1;
            res.color = graph.getNodeAttribute(src, 'color') + 'cc';
          } else {
            res.color = 'rgba(48,54,61,0.2)'; res.size = 1;
          }
        }
        return res;
      }

      // In diff mode, edge color is already set by loadDiffView; just use it
      if (isDiffMode) {
        return res;
      }
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

  sigmaInstance.on('clickNode', ({ node }) => {
    if (isSchemaMode) { selectSchemaTypeNode(node); return; }
    selectNode(node);
  });
  sigmaInstance.on('doubleClickNode', ({ node }) => {
    if (isSchemaMode) { exitSchemaView(node); return; }
    expandNode(node);
  });
  sigmaInstance.on('rightClickNode', ({ node, event }) => {
    event.preventDefault();
    selectNode(node);
    showContextMenu(node, event.clientX, event.clientY);
  });
  sigmaInstance.on('clickStage', () => deselectNode());

  // Hover card on canvas nodes
  sigmaInstance.on('enterNode', ({ node }) => {
    if (dragState) return; // don't show while dragging
    const d = nodeData[node] || {};
    const attrs = graph.getNodeAttributes(node);
    const vp = sigmaInstance.graphToViewport({ x: attrs.x, y: attrs.y });
    const container = document.getElementById('sigma-container');
    const rect = container.getBoundingClientRect();
    showHoverCard(
      node,
      rect.left + vp.x,
      rect.top + vp.y,
      attrs.color || '#8b949e',
      d.type || attrs.nodeType || '',
      attrs.label || '',
      d.properties || null
    );
  });
  sigmaInstance.on('leaveNode', () => hideHoverCard(80));

  // Re-render on camera move so semantic zoom labels update; hide hover card during pan
  sigmaInstance.getCamera().on('updated', () => {
    sigmaInstance.refresh();
    hideHoverCard();
  });

  enableNodeDrag();
}

// ── Node selection ────────────────────────────────────────────────────────
// Smoothly animate camera to frame the selected node + all its visible neighbors.
// Works entirely in Sigma's normalized display coordinates to avoid circular
// dependency on the current camera state.
function zoomToNodeNeighborhood(nodeId) {
  if (!sigmaInstance || !graph.hasNode(nodeId)) return;
  const neighbors = graph.neighbors(nodeId).filter(n => !graph.getNodeAttribute(n, 'hidden'));
  const nodes = [nodeId, ...neighbors];
  if (nodes.length === 0) return;

  // Single node — center on it with a moderate zoom
  if (nodes.length === 1) {
    const nd = sigmaInstance.getNodeDisplayData(nodeId);
    if (!nd) return;
    sigmaInstance.getCamera().animate(
      { x: nd.x, y: nd.y, ratio: 0.3 },
      { duration: 600, easing: 'cubicInOut' }
    );
    return;
  }

  // Compute bounding box in normalized display coords (0..1 range)
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of nodes) {
    const nd = sigmaInstance.getNodeDisplayData(n);
    if (!nd) continue;
    if (nd.x < minX) minX = nd.x;
    if (nd.x > maxX) maxX = nd.x;
    if (nd.y < minY) minY = nd.y;
    if (nd.y > maxY) maxY = nd.y;
  }

  // Center of the neighborhood
  const cx = (minX + maxX) / 2;
  const cy = (minY + maxY) / 2;

  // Bbox span in normalized coords
  const bboxW = (maxX - minX) || 0.01;
  const bboxH = (maxY - minY) || 0.01;

  // Account for viewport aspect ratio — Sigma's ratio maps to the smaller
  // dimension, so we need to scale the bbox axis that's tighter.
  const container = document.getElementById('sigma-container');
  const aspect = container.clientWidth / container.clientHeight;

  // In Sigma v3, camera ratio = fraction of the normalized space visible.
  // For a square viewport: visible range ≈ ratio in both axes.
  // For non-square: visible width ≈ ratio * aspect, visible height ≈ ratio.
  // We want the bbox to fill ~60% of the viewport in the larger axis.
  const fill = 0.6;
  const ratioFromW = bboxW / (fill * aspect);
  const ratioFromH = bboxH / fill;
  let newRatio = Math.max(ratioFromW, ratioFromH);
  // Clamp: don't zoom too far in (< 0.08) or too far out (> 2)
  newRatio = Math.max(0.08, Math.min(newRatio, 2));

  sigmaInstance.getCamera().animate(
    { x: cx, y: cy, ratio: newRatio },
    { duration: 600, easing: 'cubicInOut' }
  );
}

function selectNode(id) {
  if (!graph.hasNode(id)) return;
  hideHoverCard();
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
  document.getElementById('panel-title').textContent = 'Node details';

  // Restore panel footer visibility (may have been hidden in schema mode)
  const panelFooter = document.querySelector('#right-panel .right-panel-inner > .p-3.border-t');
  if (panelFooter) panelFooter.style.display = '';

  // Use htmx.ajax to directly fetch node detail into the panel body
  htmx.ajax('GET', `/htmx/node-detail?nodeId=${encodeURIComponent(entityId)}`, {
    target: '#panel-body',
    swap: 'innerHTML',
  });

  $panel.classList.add('open');
  setTimeout(() => {
    sigmaInstance?.resize();
    // After resize settles, zoom to show node + neighbors
    setTimeout(() => zoomToNodeNeighborhood(id), 50);
  }, 210);

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
  const newDepth = parseInt(depthBtn.id.replace('depth-', ''), 10);
  if (newDepth === expandDepth) return;
  expandDepth = newDepth;
  document.querySelectorAll('.depth-btn').forEach(b => {
    const active = b.id === depthBtn.id;
    b.classList.toggle('bg-gh-accent', active);
    b.classList.toggle('text-white', active);
    b.classList.toggle('bg-gh-surface2', !active);
    b.classList.toggle('text-gh-muted', !active);
  });
  // If a node is already expanded, re-expand with the new depth
  if (expandedNode && selectedNode === expandedNode) {
    expandNode(selectedNode);
  }
});

document.getElementById('btn-expand').addEventListener('click', async () => {
  if (!selectedNode) return;
  if (expandedNode === selectedNode && expandedNodeDepth === expandDepth) {
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
  expandedNode = null; expandedNodeDepth = 0; expandedNodeIds.clear(); expandedEdgeKeys.clear();
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
let expandedNodeDepth = 0;   // depth used for the last expand
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
  expandedNodeDepth = 0;
  expandedNodeIds.clear();
  expandedEdgeKeys.clear();
  updateStats();
  if (sigmaInstance) sigmaInstance.refresh();
  updateExpandBtn();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
}

async function expandNode(nodeId) {
  // Collapse previous expand (different node OR same node at different depth)
  if (expandedNode) collapseExpanded();

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
    expandedNodeDepth = expandDepth;
    const added = graph.order - prevOrder;
    runLayout({ resetPositions: prevOrder === 0 });
    showToast(added > 0 ? `+${added} node${added !== 1 ? 's' : ''} added (depth ${expandDepth})` : 'No new neighbors found');
    updateExpandBtn();
  } catch (e) { showToast('Expand failed: ' + e.message, 3500); }
  finally { stopLoading(); }
}

// ── Load nodes by type ────────────────────────────────────────────────────
async function loadNodesByType(type) {
  startLoading();
  try {
    const params = new URLSearchParams({ type, limit: '100' });
    const res = await fetch(appendBranch(`/proxy/api/graph/objects/search?${params}`));
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
      const res = await fetch(appendBranch(`/proxy/api/graph/objects/search?${params}`));
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

// ── Relation row hover card ───────────────────────────────────────────────
// Delegated mouseenter/mouseleave on [data-action="navigate-node"] rows in the
// right panel. Shows the shared hover card with the neighbor's info.
document.addEventListener('mouseover', (e) => {
  const row = e.target.closest('[data-action="navigate-node"]');
  if (!row) return;
  const nid = row.dataset.nodeId;
  if (!nid) return;

  // Gather info — prefer in-graph data, fall back to row content
  const d = nodeData[nid];
  const typeName = d?.type || row.querySelector('.text-gh-muted.shrink-0')?.textContent?.trim() || '';
  const name = d?.properties?.name || d?.properties?.title || d?.key ||
    row.querySelector('.text-gh-text.truncate')?.textContent?.trim() || nid.slice(0, 8);
  const color = d ? typeColor(typeName) : (row.querySelector('.rounded-full')?.style.background || '#8b949e');
  const props = d?.properties || null;

  const rect = row.getBoundingClientRect();
  showHoverCard(nid, rect.left - 232, rect.top + rect.height / 2, color, typeName, name, props);
});

document.addEventListener('mouseout', (e) => {
  const row = e.target.closest('[data-action="navigate-node"]');
  if (row) hideHoverCard(120);
});

// ── Branch picker ─────────────────────────────────────────────────────────
const $branchBtn = document.getElementById('branch-btn');
const $branchDropdown = document.getElementById('branch-dropdown');
const $branchList = document.getElementById('branch-list');
const $branchLabel = document.getElementById('branch-label');
let branchesLoaded = false;

// Set initial label
if (currentBranchID) {
  $branchLabel.textContent = currentBranchID;
} else {
  $branchLabel.textContent = 'main';
}

async function loadBranches() {
  if (branchesLoaded) return;
  try {
    const res = await fetch('/htmx/branches');
    const branches = await res.json();
    $branchList.innerHTML = '';

    // Main graph option
    const mainItem = document.createElement('button');
    mainItem.className = 'w-full text-left px-3 py-1.5 hover:bg-gh-surface2 text-gh-text cursor-pointer flex items-center gap-2'
      + (!currentBranchID ? ' bg-gh-surface2' : '');
    mainItem.innerHTML = `<span class="w-1.5 h-1.5 rounded-full ${!currentBranchID ? 'bg-gh-accent' : 'bg-transparent'}"></span>main`;
    mainItem.addEventListener('click', () => switchBranch('', 'main'));
    $branchList.appendChild(mainItem);

    if (branches.length > 0) {
      const sep = document.createElement('div');
      sep.className = 'my-1 border-t border-gh-border';
      $branchList.appendChild(sep);
    }

    for (const b of branches) {
      const name = b.name || b.id;
      const isActive = currentBranchID === b.id || currentBranchID === b.name;
      const item = document.createElement('button');
      item.className = 'w-full text-left px-3 py-1.5 hover:bg-gh-surface2 text-gh-text cursor-pointer flex items-center gap-2'
        + (isActive ? ' bg-gh-surface2' : '');
      item.innerHTML = `<span class="w-1.5 h-1.5 rounded-full ${isActive ? 'bg-gh-accent' : 'bg-transparent'}"></span>`
        + `<span class="truncate">${escHtml(name)}</span>`;
      item.addEventListener('click', () => switchBranch(b.id, name));
      $branchList.appendChild(item);
    }

    branchesLoaded = true;
  } catch (e) {
    $branchList.innerHTML = `<div class="px-3 py-2 text-red-400 text-[11px]">Failed to load branches</div>`;
  }
}

function switchBranch(branchID, label) {
  if (branchID === currentBranchID) {
    $branchDropdown.classList.add('hidden');
    return;
  }

  // Exit diff mode before switching (don't restore pre-diff state)
  if (isDiffMode) {
    isDiffMode = false;
    diffStatusMap.clear();
    preDiffGraphState = null;
    hideDiffLegend();
  }

  // Exit schema mode before switching
  if (isSchemaMode) {
    isSchemaMode = false;
    selectedSchemaType = null;
    schemaData = null;
    preSchemaGraphState = null;
    hideSchemaLegend();
    disableGraphControls(false);
    updateModeTabs();
  }

  currentBranchID = branchID;
  $branchLabel.textContent = label;
  $branchDropdown.classList.add('hidden');
  branchesLoaded = false; // force reload next time

  // Show/hide diff button based on whether we're on a branch
  const $diffBtn = document.getElementById('btn-diff');
  if ($diffBtn) {
    if (branchID) {
      $diffBtn.classList.remove('hidden');
    } else {
      $diffBtn.classList.add('hidden');
    }
    updateDiffBtn();
  }

  // Clear graph and reload everything
  stopFA2();
  graph.clear();
  nodeData = {}; edgeData = {};
  selectedNode = null;
  expandedNode = null; expandedNodeDepth = 0; expandedNodeIds.clear(); expandedEdgeKeys.clear();
  focusActive = false;
  $panel.classList.remove('open');
  hiddenNodeTypes.clear(); hiddenEdgeTypes.clear();
  Object.keys(nodeTypeCounts).forEach(k => delete nodeTypeCounts[k]);
  Object.keys(edgeTypeCounts).forEach(k => delete edgeTypeCounts[k]);

  // Force schema reload on server side by clearing cache
  // (the server will reload with the new branch_id in proxyGet)
  updateStats();
  updateExpandBtn(); updateFocusBtn();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
  showToast(branchID ? `Switched to branch: ${label}` : 'Switched to main graph');
}

$branchBtn?.addEventListener('click', () => {
  const isOpen = !$branchDropdown.classList.contains('hidden');
  if (isOpen) {
    $branchDropdown.classList.add('hidden');
  } else {
    loadBranches();
    $branchDropdown.classList.remove('hidden');
  }
});

// Close dropdown when clicking outside
document.addEventListener('click', (e) => {
  if (!$branchDropdown?.classList.contains('hidden') && !e.target.closest('#branch-picker')) {
    $branchDropdown.classList.add('hidden');
  }
});

// ── Diff mode ─────────────────────────────────────────────────────────────
async function callMergeDryRun() {
  // POST to the merge dry-run endpoint proxied through /proxy/
  const res = await fetch('/proxy/api/graph/branches/main/merge', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Project-ID': PROJECT_ID },
    body: JSON.stringify({ source_branch_id: currentBranchID, execute: false }),
  });
  if (!res.ok) throw new Error(`Merge dry-run failed: ${res.status}`);
  return res.json();
}

async function loadDiffView() {
  if (!currentBranchID) return;
  const $btn = document.getElementById('btn-diff');
  if ($btn) { $btn.classList.add('diff-loading'); $btn.disabled = true; }
  startLoading();

  try {
    const resp = await callMergeDryRun();

    // Build diff status map (objects + relationships)
    diffStatusMap.clear();
    const relDiffMap = new Map();
    for (const obj of (resp.objects || [])) {
      if (obj.status !== 'unchanged') diffStatusMap.set(obj.canonical_id, obj.status);
    }
    for (const rel of (resp.relationships || [])) {
      if (rel.status !== 'unchanged') relDiffMap.set(rel.canonical_id || rel.id, rel.status);
    }

    if (diffStatusMap.size === 0 && relDiffMap.size === 0) {
      showToast('No changes found between branch and main', 3500);
      return;
    }

    // Save pre-diff graph state
    preDiffGraphState = {
      nodes: graph.nodes().map(n => ({ id: n, attrs: { ...graph.getNodeAttributes(n) } })),
      edges: graph.edges().map(e => ({ key: e, src: graph.source(e), dst: graph.target(e), attrs: { ...graph.getEdgeAttributes(e) } })),
      nodeData: { ...nodeData },
      edgeData: { ...edgeData },
    };

    // Clear canvas
    stopFA2();
    graph.clear();
    nodeData = {}; edgeData = {};

    // Fetch each changed object from the branch
    const ids = [...diffStatusMap.keys()];
    for (const canonicalId of ids) {
      try {
        const res = await fetch(appendBranch(`/proxy/api/graph/objects/${encodeURIComponent(canonicalId)}`));
        if (!res.ok) continue;
        const obj = await res.json();
        mergeObjectResponse(obj, { forceVisible: true });
      } catch (_) {}
    }

    // Color diff nodes using their diff status color
    diffStatusMap.forEach((status, cid) => {
      if (graph.hasNode(cid)) {
        graph.setNodeAttribute(cid, 'color', DIFF_COLORS[status] || DIFF_COLORS.fast_forward);
        graph.setNodeAttribute(cid, 'diffStatus', status);
      }
    });

    // Load relationships between diff nodes (best-effort)
    if (ids.length > 0) {
      try {
        const expandResp = await api('/api/graph/expand', { root_ids: ids, max_depth: 1, max_nodes: 500 });
        const v2c = {};
        (expandResp.nodes || []).forEach(n => {
          const cid = n.canonical_id || n.entity_id;
          const vid = n.id || n.version_id;
          if (cid && vid) v2c[vid] = cid;
        });
        (expandResp.edges || []).forEach(e => {
          const src = v2c[e.src_id] || e.src_id;
          const dst = v2c[e.dst_id] || e.dst_id;
          // Only add edges between diff nodes
          if (diffStatusMap.has(src) && diffStatusMap.has(dst)) {
            mergeEdge({ ...e, src_id: src, dst_id: dst }, { forceVisible: true });
          }
        });
      } catch (_) {}
    }

    // Color edges by source node diff status
    graph.forEachEdge((key) => {
      const src = graph.source(key);
      const status = diffStatusMap.get(src);
      if (status) graph.setEdgeAttribute(key, 'color', (DIFF_COLORS[status] || DIFF_COLORS.fast_forward) + 'cc');
    });

    isDiffMode = true;
    updateStats();
    if (sigmaInstance) sigmaInstance.refresh();
    runLayout({ resetPositions: true });
    syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
    updateDiffBtn();
    showDiffLegend();
    showToast(`Diff mode: ${diffStatusMap.size} changed object${diffStatusMap.size !== 1 ? 's' : ''}`);
  } catch (e) {
    showToast('Diff failed: ' + e.message, 4000);
  } finally {
    stopLoading();
    if ($btn) { $btn.classList.remove('diff-loading'); $btn.disabled = false; }
  }
}

function exitDiffView() {
  if (!isDiffMode) return;
  isDiffMode = false;
  diffStatusMap.clear();

  // Clear diff canvas
  stopFA2();
  graph.clear();
  nodeData = {}; edgeData = {};

  // Restore pre-diff state
  if (preDiffGraphState) {
    preDiffGraphState.nodes.forEach(({ id, attrs }) => {
      if (!graph.hasNode(id)) graph.addNode(id, attrs);
    });
    preDiffGraphState.edges.forEach(({ key, src, dst, attrs }) => {
      if (graph.hasNode(src) && graph.hasNode(dst) && !graph.hasEdge(key)) {
        try { graph.addEdgeWithKey(key, src, dst, attrs); } catch (_) {}
      }
    });
    Object.assign(nodeData, preDiffGraphState.nodeData);
    Object.assign(edgeData, preDiffGraphState.edgeData);
    preDiffGraphState = null;
  }

  updateStats();
  if (sigmaInstance) sigmaInstance.refresh();
  updateDiffBtn();
  hideDiffLegend();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');
}

function updateDiffBtn() {
  const $btn = document.getElementById('btn-diff');
  if (!$btn) return;
  if (isDiffMode) {
    $btn.textContent = '⊟ Exit Diff';
    $btn.classList.add('border-yellow-500', 'text-yellow-400');
    $btn.classList.remove('text-gh-muted');
  } else {
    $btn.textContent = '⊞ Diff';
    $btn.classList.remove('border-yellow-500', 'text-yellow-400');
    $btn.classList.add('text-gh-muted');
  }
}

function showDiffLegend() {
  const $legend = document.getElementById('diff-legend');
  if ($legend) $legend.classList.remove('hidden');
}

function hideDiffLegend() {
  const $legend = document.getElementById('diff-legend');
  if ($legend) $legend.classList.add('hidden');
}

// Wire diff button
document.getElementById('btn-diff')?.addEventListener('click', () => {
  if (isDiffMode) exitDiffView(); else loadDiffView();
});

// ── Schema mode ───────────────────────────────────────────────────────────
async function loadSchemaView() {
  if (isSchemaMode) return;

  // Exit diff mode first if active
  if (isDiffMode) exitDiffView();

  startLoading();
  try {
    // Fetch compiled-types + registry in parallel via proxy
    const [ctRes, regRes] = await Promise.all([
      fetch(`/proxy/api/schemas/projects/${PROJECT_ID}/compiled-types`),
      fetch(`/proxy/api/schema-registry/projects/${PROJECT_ID}`),
    ]);
    const compiled = await ctRes.json();
    const registry = await regRes.json();

    // Build type set from registry (objectTypes is empty, so use registry)
    const regByType = {};
    for (const e of (registry || [])) {
      if (e.type) regByType[e.type] = e;
    }

    const rels = compiled.relationshipTypes || [];

    // Also gather types referenced in rels but missing from registry
    const allTypeNames = new Set(Object.keys(regByType));
    for (const r of rels) {
      if (r.sourceType) allTypeNames.add(r.sourceType);
      if (r.targetType) allTypeNames.add(r.targetType);
    }

    schemaData = { types: [...allTypeNames], rels };

    // Save pre-schema graph state
    preSchemaGraphState = {
      nodes: graph.nodes().map(n => ({ id: n, attrs: { ...graph.getNodeAttributes(n) } })),
      edges: graph.edges().map(e => ({ key: e, src: graph.source(e), dst: graph.target(e), attrs: { ...graph.getEdgeAttributes(e) } })),
      nodeData: { ...nodeData },
      edgeData: { ...edgeData },
      selectedNode,
    };

    // Clear canvas
    stopFA2();
    graph.clear();
    nodeData = {}; edgeData = {};
    selectedNode = null; selectedSchemaType = null;
    $panel.classList.remove('open');

    // Build type nodes
    for (const typeName of allTypeNames) {
      const reg = regByType[typeName];
      const color = typeColor(typeName);
      const propCount = reg?.json_schema?.properties ? Object.keys(reg.json_schema.properties).length : 0;
      graph.addNode(typeName, {
        label: typeName,
        size: 22,
        color,
        x: Math.random() * 10 - 5,
        y: Math.random() * 10 - 5,
        nodeType: typeName,
        typeInitial: typeIcon(typeName),
        isSchemaNode: true,
        propCount,
      });
    }

    // Build relationship edges
    for (const rel of rels) {
      const src = rel.sourceType;
      const dst = rel.targetType;
      const label = rel.label || rel.name || '';
      if (!src || !dst || !graph.hasNode(src) || !graph.hasNode(dst)) continue;
      // Self-loops: skip in graph (note in detail panel) since allowSelfLoops is false
      if (src === dst) continue;
      const key = `schema__${rel.name}__${src}__${dst}`;
      if (!graph.hasEdge(key)) {
        try {
          graph.addEdgeWithKey(key, src, dst, {
            label,
            size: 2,
            color: typeColor(src) + '88',
            forceLabel: true,
          });
        } catch (_) {}
      }
    }

    isSchemaMode = true;
    updateModeTabs();
    updateStats();
    disableGraphControls(true);
    showSchemaLegend();

    // Layout
    runLayout({ resetPositions: true });
    showToast(`Schema: ${allTypeNames.size} types, ${rels.length} relationships`);
  } catch (e) {
    showToast('Schema load failed: ' + e.message, 3500);
  } finally {
    stopLoading();
  }
}

function exitSchemaView(loadTypeName) {
  if (!isSchemaMode) return;
  isSchemaMode = false;
  selectedSchemaType = null;
  schemaData = null;

  // Clear schema canvas
  stopFA2();
  graph.clear();
  nodeData = {}; edgeData = {};
  $panel.classList.remove('open');

  // Restore panel footer visibility
  const panelFooter = document.querySelector('#right-panel .right-panel-inner > .p-3.border-t');
  if (panelFooter) panelFooter.style.display = '';

  // Restore pre-schema state
  if (preSchemaGraphState) {
    preSchemaGraphState.nodes.forEach(({ id, attrs }) => {
      if (!graph.hasNode(id)) graph.addNode(id, attrs);
    });
    preSchemaGraphState.edges.forEach(({ key, src, dst, attrs }) => {
      if (graph.hasNode(src) && graph.hasNode(dst) && !graph.hasEdge(key)) {
        try { graph.addEdgeWithKey(key, src, dst, attrs); } catch (_) {}
      }
    });
    Object.assign(nodeData, preSchemaGraphState.nodeData);
    Object.assign(edgeData, preSchemaGraphState.edgeData);
    selectedNode = preSchemaGraphState.selectedNode;
    preSchemaGraphState = null;
  }

  updateModeTabs();
  updateStats();
  disableGraphControls(false);
  hideSchemaLegend();
  if (sigmaInstance) sigmaInstance.refresh();
  syncHiddenInputs(); htmx.trigger(document.body, 'refreshFilters');

  // If asked to load a specific type, do it
  if (loadTypeName) {
    loadNodesByType(loadTypeName);
  }
}

function updateModeTabs() {
  const $tabGraph = document.getElementById('tab-graph');
  const $tabSchema = document.getElementById('tab-schema');
  if (!$tabGraph || !$tabSchema) return;
  if (isSchemaMode) {
    $tabGraph.className = 'px-3 py-1 text-[11px] font-medium bg-gh-surface2 text-gh-muted cursor-pointer border-none border-r border-gh-border hover:text-gh-text';
    $tabSchema.className = 'px-3 py-1 text-[11px] font-medium bg-gh-accent text-white cursor-pointer border-none border-l border-gh-border';
  } else {
    $tabGraph.className = 'px-3 py-1 text-[11px] font-medium bg-gh-accent text-white cursor-pointer border-none';
    $tabSchema.className = 'px-3 py-1 text-[11px] font-medium bg-gh-surface2 text-gh-muted cursor-pointer border-none border-l border-gh-border hover:text-gh-text';
  }
}

function disableGraphControls(disable) {
  const ids = ['btn-load-all', 'btn-diff', 'search-btn', 'search-input'];
  ids.forEach(id => {
    const el = document.getElementById(id);
    if (el) {
      el.disabled = disable;
      if (disable) el.classList.add('opacity-40', 'pointer-events-none');
      else el.classList.remove('opacity-40', 'pointer-events-none');
    }
  });
}

function selectSchemaTypeNode(typeName) {
  selectedSchemaType = typeName;
  selectedNode = typeName; // for reducer highlighting
  if (sigmaInstance) sigmaInstance.refresh();

  // Fetch schema type detail via HTMX
  document.getElementById('panel-title').textContent = 'Type details';
  htmx.ajax('GET', `/htmx/schema-type-detail?type=${encodeURIComponent(typeName)}`, {
    target: '#panel-body',
    swap: 'innerHTML',
  });

  // Hide the panel footer (expand/copy/focus buttons) — not relevant for schema types
  const panelFooter = document.querySelector('#right-panel .right-panel-inner > .p-3.border-t');
  if (panelFooter) panelFooter.style.display = 'none';

  $panel.classList.add('open');
  setTimeout(() => {
    sigmaInstance?.resize();
    setTimeout(() => zoomToNodeNeighborhood(typeName), 50);
  }, 210);
}

function showSchemaLegend() {
  const $legend = document.getElementById('schema-legend');
  if ($legend) $legend.classList.remove('hidden');
}

function hideSchemaLegend() {
  const $legend = document.getElementById('schema-legend');
  if ($legend) $legend.classList.add('hidden');
}

// Wire schema mode tabs
document.getElementById('tab-graph')?.addEventListener('click', () => {
  if (isSchemaMode) exitSchemaView();
});
document.getElementById('tab-schema')?.addEventListener('click', () => {
  if (!isSchemaMode) loadSchemaView();
});

// Wire "Browse objects" button in schema detail panel (delegated click)
document.addEventListener('click', (e) => {
  const browseBtn = e.target.closest('[data-action="browse-objects"]');
  if (browseBtn) {
    const typeName = browseBtn.dataset.type;
    if (typeName && isSchemaMode) exitSchemaView(typeName);
    return;
  }
});

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

// ── Debug helpers (expose for DevTools testing) ───────────────────────────
window.__ge = { get graph() { return graph; }, get sigma() { return sigmaInstance; },
  selectNode, deselectNode, zoomToNodeNeighborhood, nodeData,
  loadDiffView, exitDiffView, get isDiffMode() { return isDiffMode; }, get diffStatusMap() { return diffStatusMap; },
  loadSchemaView, exitSchemaView, get isSchemaMode() { return isSchemaMode; } };
