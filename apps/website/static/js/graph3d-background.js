/**
 * Graph3DBackground - Animated 3D particle graph visualization
 * Ported from React component for standalone use
 */

class Graph3DSlow {
  constructor(svg) {
    this.svg = svg;
    this.width = svg.clientWidth;
    this.height = svg.clientHeight;
    this.nodes = [];
    this.edges = [];
    this.maxNodes = 600;
    this.nodeRadius = 4;
    this.perspective = 0.001;
    this.rotation = 0;
    this.edgeMap = new Map();
    this.animationId = null;

    // Asteroid belt theme colors (using OKLCH values)
    this.edgeColors = [
      'rgba(180, 140, 90, 0.25)', // Bronze/metallic accent
      'rgba(150, 155, 165, 0.2)', // Cool steel gray
      'rgba(120, 115, 110, 0.2)', // Warm gray
    ];

    this.init();
    this.animate = this.animate.bind(this);
    this.animationId = requestAnimationFrame(this.animate);
  }

  init() {
    for (let i = 0; i < 350; i++) this.addNode();
    this.updateEdges();
  }

  addNode() {
    const node = {
      x: Math.random() * this.width,
      y: Math.random() * this.height,
      z: Math.random() * 2000 - 1000,
    };
    this.nodes.push(node);
    if (this.nodes.length > this.maxNodes) this.nodes.shift();
  }

  updateEdges() {
    const newEdges = [];
    const maxConnections = 2;
    const currentKeys = new Set();

    for (let i = 0; i < this.nodes.length; i++) {
      const distances = [];
      for (let j = 0; j < this.nodes.length; j++) {
        if (i === j) continue;
        const dx = this.nodes[i].x - this.nodes[j].x;
        const dy = this.nodes[i].y - this.nodes[j].y;
        const dz = this.nodes[i].z - this.nodes[j].z;
        const d = Math.sqrt(dx * dx + dy * dy + dz * dz);
        distances.push({ j, d });
      }
      distances.sort((a, b) => a.d - b.d);

      for (let k = 0; k < Math.min(maxConnections, distances.length); k++) {
        const aIdx = i;
        const bIdx = distances[k].j;
        const key = aIdx < bIdx ? `${aIdx}-${bIdx}` : `${bIdx}-${aIdx}`;
        currentKeys.add(key);

        let colorIdx;
        if (this.edgeMap.has(key)) {
          colorIdx = this.edgeMap.get(key);
        } else {
          colorIdx = Math.floor(Math.random() * this.edgeColors.length);
          this.edgeMap.set(key, colorIdx);
        }
        newEdges.push({ a: aIdx, b: bIdx, colorIdx });
      }
    }

    // Remove edges that no longer exist
    const keysToDelete = [];
    this.edgeMap.forEach((_, key) => {
      if (!currentKeys.has(key)) keysToDelete.push(key);
    });
    keysToDelete.forEach((key) => this.edgeMap.delete(key));

    this.edges = newEdges;
  }

  project(point) {
    const scale = 1 / (1 + point.z * this.perspective);
    return {
      x: point.x * scale + (this.width / 2) * (1 - scale),
      y: point.y * scale + (this.height / 2) * (1 - scale),
      scale,
    };
  }

  rotateY(point) {
    const sinA = Math.sin(this.rotation);
    const cosA = Math.cos(this.rotation);
    const x = point.x - this.width / 2;
    const z = point.z;
    const rotatedX = cosA * x + sinA * z;
    const rotatedZ = -sinA * x + cosA * z;
    return { x: rotatedX + this.width / 2, y: point.y, z: rotatedZ };
  }

  clear() {
    while (this.svg.firstChild) this.svg.removeChild(this.svg.firstChild);
  }

  draw() {
    this.clear();

    // Draw edges first
    for (const e of this.edges) {
      const a = this.rotateY(this.nodes[e.a]);
      const b = this.rotateY(this.nodes[e.b]);
      const p1 = this.project(a);
      const p2 = this.project(b);
      const line = document.createElementNS(
        'http://www.w3.org/2000/svg',
        'line'
      );
      line.setAttribute('x1', p1.x.toString());
      line.setAttribute('y1', p1.y.toString());
      line.setAttribute('x2', p2.x.toString());
      line.setAttribute('y2', p2.y.toString());
      line.setAttribute('stroke', this.edgeColors[e.colorIdx]);
      line.setAttribute('stroke-width', '1');
      this.svg.appendChild(line);
    }

    // Draw nodes with bronze/metallic accent color
    for (const n of this.nodes) {
      const rp = this.rotateY(n);
      const p = this.project(rp);
      const circle = document.createElementNS(
        'http://www.w3.org/2000/svg',
        'circle'
      );
      circle.setAttribute('cx', p.x.toString());
      circle.setAttribute('cy', p.y.toString());
      const r = this.nodeRadius * p.scale;
      circle.setAttribute('r', Math.max(r, 0.5).toString());
      // Bronze accent with glow
      circle.setAttribute('fill', 'rgba(180, 140, 90, 0.7)');
      this.svg.appendChild(circle);
    }
  }

  animate() {
    // Slower rotation
    this.rotation += 0.0002;

    // Drift nodes with slower speed
    const speed = Math.random() * 0.2;
    for (const n of this.nodes) {
      n.x += (Math.random() - 0.5) * speed;
      n.y += (Math.random() - 0.5) * speed;
      n.z += (Math.random() - 0.5) * speed * 2;

      // Wrap around
      if (n.x < 0) n.x += this.width;
      if (n.x > this.width) n.x -= this.width;
      if (n.y < 0) n.y += this.height;
      if (n.y > this.height) n.y -= this.height;
      if (n.z < -1000) n.z = 1000;
      if (n.z > 1000) n.z = -1000;
    }

    // Slower random creation/destruction
    const changeRate = Math.random() * 0.01;

    if (Math.random() < changeRate) this.addNode();
    if (Math.random() < changeRate) this.addNode();

    if (Math.random() < changeRate && this.nodes.length > 10) {
      this.nodes.splice(Math.floor(Math.random() * this.nodes.length), 1);
    }

    this.updateEdges();
    this.draw();
    this.animationId = requestAnimationFrame(this.animate);
  }

  destroy() {
    if (this.animationId) {
      cancelAnimationFrame(this.animationId);
      this.animationId = null;
    }
    this.clear();
  }
}

// Initialize Graph3D backgrounds on page load
document.addEventListener('DOMContentLoaded', () => {
  const backgrounds = document.querySelectorAll('.graph3d-background');
  const instances = [];

  backgrounds.forEach((container) => {
    const svg = container.querySelector('svg');
    if (svg) {
      const graph = new Graph3DSlow(svg);
      instances.push(graph);
    }
  });

  // Cleanup on page unload
  window.addEventListener('beforeunload', () => {
    instances.forEach((graph) => graph.destroy());
  });
});
