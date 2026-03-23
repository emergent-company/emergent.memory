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

function nodeSize(deg) { return Math.min(8 + Math.sqrt(deg) * 3.5, 36); }

function mergeNode(n, { forceVisible = false } = {}) {
  const id = n.canonical_id || n.entity_id || n.id;
  if (!id) return;
  const type = n.type || 'unknown';
  const color = typeColor(type);
  const props = n.properties || {};
  const label = props.name || props.title || n.key || (type + ' ' + String(id).slice(0, 6));
  if (!graph.hasNode(id)) {
    graph.addNode(id, {
      label, size: 8, color,
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
      label, size: 8, color,
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
}

function drawNodeHoverLabel(context, data, settings) {
  drawNodeIcon(context, data);

  const { size, x, y, color, label, typeInitial } = data;
  if (!label) return;

  const icon = typeInitial || (label || '?').charAt(0).toUpperCase();
  const isEmoji = [...icon].length > 1;
  const ls = 11;
  const pad = 5;
  const dotR = 7;
  const iconFs = isEmoji ? 10 : 8;
  const gap = 5;

  context.save();
  context.font = `500 ${ls}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  const tw = context.measureText(label).width;

  const chipW = pad + dotR * 2 + gap + tw + pad;
  const chipH = ls + 8;
  const chipX = x + size + 6;
  const chipY = y - chipH / 2;

  context.fillStyle = 'rgba(13,17,23,0.88)';
  context.strokeStyle = 'rgba(48,54,61,0.7)';
  context.lineWidth = 0.8;
  context.beginPath();
  context.roundRect(chipX, chipY, chipW, chipH, chipH / 2);
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

  context.font = `500 ${ls}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
  context.textAlign = 'left';
  context.textBaseline = 'middle';
  context.fillStyle = '#e6edf3';
  context.fillText(label, chipX + pad + dotR * 2 + gap, y);

  context.restore();
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
      if (selectedNode !== null) {
        if (!graph.hasNode(selectedNode)) {
          // Selected node was removed — treat as no selection
          res.label = '';
        } else if (node === selectedNode) {
          res.highlighted = true; res.size *= 1.5; res.zIndex = 2;
        } else if (graph.neighbors(selectedNode).includes(node)) {
          res.color = data.color;
          res.label = '';
        } else {
          res.color = '#1c2128'; res.label = ''; res.opacity = 0.3;
        }
      } else {
        res.label = '';
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

  sigmaInstance.on('clickNode', ({ node }) => { selectNode(node); expandNode(node); });
  sigmaInstance.on('clickStage', () => deselectNode());
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
    const resp = await api('/api/graph/expand', { root_ids: [id], max_depth: 1, max_nodes: 200 });

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

// ── Search ────────────────────────────────────────────────────────────────
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
