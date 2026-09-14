// usage-charts.js — dependency-free SVG bar charts for the usage dashboard.
//
// Reads the #usage-timeseries JSON payload embedded by the page
// ({tokens:[{day,value}], sessions:[{day,value}]}) and renders one bar chart
// into each target: #usage-token-chart (daily tokens) and #usage-session-chart
// (daily sessions). Bars use the daisyUI primary theme color via CSS variable
// (--color-primary), day labels the base-content variable (--color-base-content). An all-zero series
// renders a "no data" note instead of a flat chart.
(function () {
  'use strict';

  var payloadEl = document.getElementById('usage-timeseries');
  if (!payloadEl) {
    return;
  }
  var payload;
  try {
    payload = JSON.parse(payloadEl.textContent || payloadEl.text || '{}');
  } catch (err) {
    return;
  }

  var SVG_NS = 'http://www.w3.org/2000/svg';
  var W = 640;
  var H = 170;
  var PAD = 6;
  var LABEL_H = 20;
  var PLOT_H = H - LABEL_H;

  renderChart('usage-token-chart', payload.tokens, 'tokens');
  renderChart('usage-session-chart', payload.sessions, 'sessions');

  function renderChart(targetId, points, unit) {
    var target = document.getElementById(targetId);
    if (!target || !points || !points.length) {
      return;
    }
    var max = 0;
    points.forEach(function (p) {
      if (p.value > max) {
        max = p.value;
      }
    });
    if (max === 0) {
      target.innerHTML =
        '<p class="text-base-content/45 text-center text-xs">No usage in this range.</p>';
      return;
    }

    var svg = document.createElementNS(SVG_NS, 'svg');
    svg.setAttribute('viewBox', '0 0 ' + W + ' ' + H);
    svg.setAttribute('class', 'w-full h-auto block');
    svg.setAttribute('role', 'img');

    var n = points.length;
    var slot = (W - PAD * 2) / n;
    var barW = Math.max(1, Math.min(slot - 2, 18));

    points.forEach(function (p, i) {
      var barH = Math.max(2, (p.value / max) * PLOT_H);
      var x = PAD + i * slot + (slot - barW) / 2;
      var y = H - LABEL_H - barH;

      var rect = document.createElementNS(SVG_NS, 'rect');
      rect.setAttribute('x', x.toFixed(1));
      rect.setAttribute('y', y.toFixed(1));
      rect.setAttribute('width', barW.toFixed(1));
      rect.setAttribute('height', barH.toFixed(1));
      rect.setAttribute('rx', '2');
      rect.setAttribute('fill', 'var(--color-primary)');
      var title = document.createElementNS(SVG_NS, 'title');
      title.textContent = p.day + ': ' + p.value + ' ' + unit;
      rect.appendChild(title);
      svg.appendChild(rect);

      // X-axis label: show roughly every ~8th point to avoid crowding.
      if (n <= 16 || i % Math.ceil(n / 8) === 0) {
        var label = document.createElementNS(SVG_NS, 'text');
        label.setAttribute('x', (x + barW / 2).toFixed(1));
        label.setAttribute('y', String(H - 6));
        label.setAttribute('text-anchor', 'middle');
        label.setAttribute('font-size', '9');
        label.setAttribute('fill', 'var(--color-base-content)');
        label.setAttribute('opacity', '0.7');
        label.textContent = shortDay(p.day);
        svg.appendChild(label);
      }
    });

    target.innerHTML = '';
    target.appendChild(svg);
  }

  // "2026-09-01" → "Sep 1".
  function shortDay(day) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day || '');
    if (!m) {
      return day;
    }
    var months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    return months[parseInt(m[2], 10) - 1] + ' ' + parseInt(m[3], 10);
  }
})();
