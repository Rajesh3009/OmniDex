// Local Torrent Indexer frontend application
let currentTorrents = [];

document.addEventListener('DOMContentLoaded', () => {
  initEventListeners();
  loadStats();
  loadConfig();
  performSearch('');

  // Auto-refresh metrics every 3 seconds
  setInterval(loadStats, 3000);
});

function initEventListeners() {
  const searchInput = document.getElementById('search-input');
  const btnSearch = document.getElementById('btn-search');

  btnSearch.addEventListener('click', () => {
    performSearch(searchInput.value);
  });

  searchInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      performSearch(searchInput.value);
    }
  });

  // Modal controls
  document.getElementById('btn-show-graph').addEventListener('click', () => {
    openModal('modal-graph');
    initDHTGraph();
  });

  document.getElementById('btn-refresh-graph').addEventListener('click', () => {
    fetchAndRenderGraph();
  });

  document.getElementById('btn-show-torznab').addEventListener('click', () => {
    openModal('modal-torznab');
  });

  document.getElementById('btn-show-settings').addEventListener('click', () => {
    openModal('modal-settings');
  });

  document.querySelectorAll('[data-close]').forEach(btn => {
    btn.addEventListener('click', () => {
      const modalId = btn.getAttribute('data-close');
      closeModal(modalId);
    });
  });

  // Settings form submission
  document.getElementById('settings-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const payload = {
      prowlarr_url: document.getElementById('cfg-prowlarr-url').value,
      prowlarr_api_key: document.getElementById('cfg-prowlarr-key').value,
      torznab_api_key: document.getElementById('cfg-torznab-key').value,
      dht_workers: parseInt(document.getElementById('cfg-dht-workers').value, 10) || 15
    };

    try {
      const resp = await fetch('/api/v1/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (resp.ok) {
        closeModal('modal-settings');
        loadConfig();
      } else {
        alert('Failed to update config: ' + await resp.text());
      }
    } catch (err) {
      alert('Error updating config: ' + err.message);
    }
  });
}

function openModal(id) {
  document.getElementById(id).classList.add('active');
}

function closeModal(id) {
  document.getElementById(id).classList.remove('active');
}

async function loadStats() {
  try {
    const res = await fetch('/api/v1/stats');
    if (!res.ok) return;
    const data = await res.json();

    document.getElementById('stat-total-torrents').textContent = data.db.total_torrents.toLocaleString();
    document.getElementById('stat-total-size').textContent = formatBytes(data.db.total_size_bytes) + ' cataloged';
    document.getElementById('stat-dht-count').textContent = data.db.dht_count.toLocaleString();
    document.getElementById('stat-dht-announces').textContent = `${data.dht.announces_received.toLocaleString()} KRPC announces`;
    document.getElementById('stat-prowlarr-count').textContent = data.db.prowlarr_count.toLocaleString();
    
    const dhtNodesEl = document.getElementById('stat-dht-nodes');
    if (dhtNodesEl) {
      dhtNodesEl.textContent = `${data.dht.good_nodes || 0} Nodes`;
    }
    document.getElementById('stat-queue-len').textContent = `${data.dht.good_nodes || 0} good / ${data.dht.total_nodes || 0} total routing nodes`;
  } catch (e) {
    console.error('Stats error:', e);
  }
}

async function loadConfig() {
  try {
    const res = await fetch('/api/v1/config');
    if (!res.ok) return;
    const cfg = await res.json();

    document.getElementById('cfg-prowlarr-url').value = cfg.prowlarr_url || '';
    document.getElementById('cfg-prowlarr-key').value = cfg.prowlarr_api_key || '';
    document.getElementById('cfg-torznab-key').value = cfg.torznab_api_key || '';
    document.getElementById('cfg-dht-workers').value = cfg.dht_workers || 15;

    document.getElementById('torznab-key-display').textContent = cfg.torznab_api_key || 'N/A';
    document.getElementById('torznab-url-display').textContent = `${window.location.origin}/api/v1/torznab`;
  } catch (e) {
    console.error('Config fetch error:', e);
  }
}

async function performSearch(query) {
  const loading = document.getElementById('loading-spinner');
  const countText = document.getElementById('results-count-text');
  const tbody = document.getElementById('torrents-tbody');
  const emptyState = document.getElementById('empty-state');
  const queryProwlarr = document.getElementById('check-prowlarr-live').checked;

  loading.style.display = 'inline-block';

  try {
    const params = new URLSearchParams({
      q: query,
      limit: '50',
      prowlarr: queryProwlarr ? 'true' : 'false'
    });

    const res = await fetch(`/api/v1/search?${params.toString()}`);
    const data = await res.json();

    currentTorrents = data.items || [];
    renderTable(currentTorrents);

    if (query.trim() === '') {
      countText.textContent = `Showing latest ${currentTorrents.length} indexed torrents (Total: ${data.total})`;
    } else {
      countText.textContent = `Found ${data.total} results for "${query}"`;
    }

    emptyState.style.display = currentTorrents.length === 0 ? 'block' : 'none';
  } catch (err) {
    console.error('Search error:', err);
    countText.textContent = 'Error fetching search results';
  } finally {
    loading.style.display = 'none';
  }
}

function renderTable(items) {
  const tbody = document.getElementById('torrents-tbody');
  tbody.innerHTML = '';

  items.forEach((item, index) => {
    const tr = document.createElement('tr');
    tr.className = 'torrent-row';

    const sourceBadge = item.source === 'prowlarr' 
      ? '<span class="badge badge-prowlarr">Prowlarr</span>'
      : '<span class="badge badge-dht">DHT Wire</span>';

    const magnet = item.magnet_uri || `magnet:?xt=urn:btih:${item.info_hash}&dn=${encodeURIComponent(item.name)}`;

    tr.innerHTML = `
      <td class="torrent-title-cell">
        <a href="javascript:void(0)" class="torrent-title" onclick="showDetails(${index})">${escapeHtml(item.name)}</a>
        <div class="torrent-hash">${item.info_hash}</div>
      </td>
      <td><span class="badge badge-category">${item.category || 'Other'}</span></td>
      <td>${formatBytes(item.size)}</td>
      <td>${item.file_count || 1}</td>
      <td>${sourceBadge}</td>
      <td style="color: var(--text-dim); font-size: 0.8rem;">${formatDate(item.created_time)}</td>
      <td style="text-align: right;">
        <div class="action-btns" style="justify-content: flex-end;">
          <a href="${magnet}" class="btn-icon" title="Magnet Download">🧲</a>
          <button class="btn-icon" onclick="showDetails(${index})" title="Inspect Files">ℹ️</button>
        </div>
      </td>
    `;
    tbody.appendChild(tr);
  });
}

function showDetails(index) {
  const item = currentTorrents[index];
  if (!item) return;

  document.getElementById('detail-title').textContent = item.name;
  document.getElementById('detail-hash').textContent = item.info_hash;
  
  const magnet = item.magnet_uri || `magnet:?xt=urn:btih:${item.info_hash}&dn=${encodeURIComponent(item.name)}`;
  document.getElementById('detail-magnet').textContent = magnet;
  document.getElementById('detail-magnet-link').href = magnet;

  const fileListEl = document.getElementById('detail-files-list');
  fileListEl.innerHTML = '';

  let files = [];
  try {
    if (item.files_json) {
      files = JSON.parse(item.files_json);
    }
  } catch (e) {}

  if (files.length === 0) {
    files = [item.name];
  }

  files.forEach(f => {
    const d = document.createElement('div');
    d.className = 'file-item';
    d.textContent = f;
    fileListEl.appendChild(d);
  });

  openModal('modal-details');
}

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatDate(dateStr) {
  if (!dateStr || dateStr.startsWith('0001-01-01')) return 'Recently';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return 'Recently';
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function escapeHtml(str) {
  if (!str) return '';
  return str.replace(/[&<>"']/g, function(m) {
    return {
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;'
    }[m];
  });
}

// -------------------------------------------------------------
// Interactive Force-Directed DHT Peer Topology Graph
// -------------------------------------------------------------
let graphAnimId = null;
let graphNodes = [];
let graphLinks = [];
let hoveredNode = null;

function initDHTGraph() {
  fetchAndRenderGraph();
}

async function fetchAndRenderGraph() {
  const statusEl = document.getElementById('graph-status-text');
  const countBadge = document.getElementById('graph-peer-count');
  if (statusEl) statusEl.textContent = 'Querying live DHT routing table...';

  try {
    const res = await fetch('/api/v1/dht/graph');
    if (!res.ok) throw new Error('Failed to fetch graph data');
    const data = await res.json();

    const canvas = document.getElementById('dht-canvas');
    if (!canvas) return;

    // Ensure internal resolution matches client display size
    const rect = canvas.getBoundingClientRect();
    if (rect.width > 0 && rect.height > 0) {
      canvas.width = rect.width;
      canvas.height = rect.height;
    } else {
      canvas.width = 850;
      canvas.height = 480;
    }

    const ctx = canvas.getContext('2d');
    const width = canvas.width;
    const height = canvas.height;

    const rawNodes = data.nodes || [];
    const rawLinks = data.links || [];

    if (countBadge) {
      countBadge.textContent = `${rawNodes.length} Nodes in Routing Table`;
    }
    if (statusEl) {
      statusEl.textContent = `Rendering ${rawNodes.length} nodes & ${rawLinks.length} connections`;
    }

    // Initialize node positions in a radial cluster
    const nodeMap = new Map();
    graphNodes = rawNodes.map((n, idx) => {
      const isLocal = n.type === 'local';
      let x, y;
      if (isLocal) {
        x = width / 2;
        y = height / 2;
      } else {
        const angle = (idx / (rawNodes.length || 1)) * 2 * Math.PI + Math.random() * 0.2;
        const dist = 120 + Math.random() * 140;
        x = width / 2 + Math.cos(angle) * dist;
        y = height / 2 + Math.sin(angle) * dist;
      }

      const nodeObj = {
        id: n.id,
        label: n.label,
        type: n.type,
        addr: n.addr,
        x: x,
        y: y,
        vx: 0,
        vy: 0,
        radius: isLocal ? 16 : 8
      };
      nodeMap.set(n.id, nodeObj);
      return nodeObj;
    });

    graphLinks = rawLinks.map(l => ({
      source: nodeMap.get(l.source) || graphNodes[0],
      target: nodeMap.get(l.target) || graphNodes[0],
      type: l.type
    })).filter(l => l.source && l.target);

    // Setup mouse interactions
    setupCanvasInteractions(canvas);

    // Start force-directed simulation
    if (graphAnimId) cancelAnimationFrame(graphAnimId);
    let iterations = 0;

    function step() {
      // Force calculations
      const kRepulsion = 1200;
      const kSpring = 0.04;
      const springLength = 140;

      // Repulsion between all nodes
      for (let i = 0; i < graphNodes.length; i++) {
        for (let j = i + 1; j < graphNodes.length; j++) {
          const a = graphNodes[i];
          const b = graphNodes[j];
          let dx = b.x - a.x;
          let dy = b.y - a.y;
          let dist = Math.sqrt(dx * dx + dy * dy) || 1;
          if (dist < 260) {
            let force = kRepulsion / (dist * dist);
            let fx = (dx / dist) * force;
            let fy = (dy / dist) * force;
            if (a.type !== 'local') {
              a.vx -= fx;
              a.vy -= fy;
            }
            if (b.type !== 'local') {
              b.vx += fx;
              b.vy += fy;
            }
          }
        }
      }

      // Spring force along links
      for (const link of graphLinks) {
        let dx = link.target.x - link.source.x;
        let dy = link.target.y - link.source.y;
        let dist = Math.sqrt(dx * dx + dy * dy) || 1;
        let force = (dist - springLength) * kSpring;
        let fx = (dx / dist) * force;
        let fy = (dy / dist) * force;

        if (link.source.type !== 'local') {
          link.source.vx += fx;
          link.source.vy += fy;
        }
        if (link.target.type !== 'local') {
          link.target.vx -= fx;
          link.target.vy -= fy;
        }
      }

      // Update positions with damping
      const damping = 0.82;
      for (const n of graphNodes) {
        if (n.type !== 'local') {
          n.vx *= damping;
          n.vy *= damping;
          n.x += n.vx;
          n.y += n.vy;

          // Keep in bounds
          n.x = Math.max(25, Math.min(width - 25, n.x));
          n.y = Math.max(25, Math.min(height - 25, n.y));
        }
      }

      // Render
      renderCanvas(ctx, width, height);

      iterations++;
      if (iterations < 200 || hoveredNode) {
        graphAnimId = requestAnimationFrame(step);
      }
    }

    step();
  } catch (err) {
    if (statusEl) statusEl.textContent = 'Error: ' + err.message;
  }
}

function renderCanvas(ctx, width, height) {
  ctx.clearRect(0, 0, width, height);

  // Draw links
  ctx.lineWidth = 1;
  for (const l of graphLinks) {
    ctx.strokeStyle = 'rgba(59, 130, 246, 0.25)';
    ctx.beginPath();
    ctx.moveTo(l.source.x, l.source.y);
    ctx.lineTo(l.target.x, l.target.y);
    ctx.stroke();
  }

  // Draw nodes
  for (const n of graphNodes) {
    ctx.beginPath();
    ctx.arc(n.x, n.y, n.radius, 0, 2 * Math.PI);

    if (n.type === 'local') {
      ctx.fillStyle = '#06b6d4';
      ctx.shadowColor = 'rgba(6, 182, 212, 0.6)';
      ctx.shadowBlur = 15;
    } else {
      ctx.fillStyle = '#a855f7';
      ctx.shadowColor = 'rgba(168, 85, 247, 0.4)';
      ctx.shadowBlur = 6;
    }

    if (hoveredNode === n) {
      ctx.lineWidth = 3;
      ctx.strokeStyle = '#fff';
      ctx.stroke();
    }

    ctx.fill();
    ctx.shadowBlur = 0; // reset
  }

  // Draw node labels on hover or local node
  for (const n of graphNodes) {
    if (n.type === 'local' || hoveredNode === n) {
      ctx.font = '10px Inter, sans-serif';
      ctx.fillStyle = '#f8fafc';
      ctx.textAlign = 'center';
      ctx.fillText(n.label, n.x, n.y - n.radius - 6);
    }
  }
}

function setupCanvasInteractions(canvas) {
  canvas.onmousemove = (e) => {
    const rect = canvas.getBoundingClientRect();
    const mx = (e.clientX - rect.left) * (canvas.width / rect.width);
    const my = (e.clientY - rect.top) * (canvas.height / rect.height);

    hoveredNode = null;
    for (const n of graphNodes) {
      const dx = n.x - mx;
      const dy = n.y - my;
      if (Math.sqrt(dx * dx + dy * dy) < n.radius + 5) {
        hoveredNode = n;
        canvas.style.cursor = 'pointer';
        break;
      }
    }
    if (!hoveredNode) {
      canvas.style.cursor = 'default';
    }
    const ctx = canvas.getContext('2d');
    renderCanvas(ctx, canvas.width, canvas.height);
  };
}

