// Local Torrent Indexer frontend application
let currentTorrents = [];
let lastTotalPages = 1;

const filterState = {
  query: '',
  category: 'all',
  source: 'all',
  sizePreset: 'all',
  minSize: 0,
  maxSize: 0,
  sort: 'created_desc',
  page: 1,
  limit: 20,
  offset: 0
};

document.addEventListener('DOMContentLoaded', () => {
  initEventListeners();
  loadStats();
  loadConfig();
  performSearch();

  // Auto-refresh metrics every 3 seconds
  setInterval(loadStats, 3000);
});

function initEventListeners() {
  const searchInput = document.getElementById('search-input');
  const btnSearch = document.getElementById('btn-search');

  btnSearch.addEventListener('click', () => {
    filterState.query = searchInput.value;
    filterState.page = 1;
    performSearch();
  });

  searchInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      filterState.query = searchInput.value;
      filterState.page = 1;
      performSearch();
    }
  });

  // Category filter pills
  document.querySelectorAll('#category-pills .pill-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const category = btn.getAttribute('data-category');
      filterState.page = 1;
      setCategoryFilter(category);
    });
  });

  // Source selector
  const sourceSelect = document.getElementById('filter-source');
  if (sourceSelect) {
    sourceSelect.addEventListener('change', () => {
      filterState.source = sourceSelect.value;
      filterState.page = 1;
      performSearch();
    });
  }

  // Size range selector
  const sizeSelect = document.getElementById('filter-size');
  if (sizeSelect) {
    sizeSelect.addEventListener('change', () => {
      setSizePreset(sizeSelect.value);
      filterState.page = 1;
      performSearch();
    });
  }

  // Sort selector
  const sortSelect = document.getElementById('sort-select');
  if (sortSelect) {
    sortSelect.addEventListener('change', () => {
      filterState.sort = sortSelect.value;
      filterState.page = 1;
      updateSortIndicators();
      performSearch();
    });
  }

  // Table header sort clicks
  document.querySelectorAll('.th-sortable').forEach(th => {
    th.addEventListener('click', () => {
      const key = th.getAttribute('data-sort-key');
      filterState.page = 1;
      handleHeaderSort(key);
    });
  });

  // Reset filters button
  const btnReset = document.getElementById('btn-reset-filters');
  if (btnReset) {
    btnReset.addEventListener('click', () => {
      resetFilters();
    });
  }

  // Pagination limit selector
  const limitSelect = document.getElementById('select-page-limit');
  if (limitSelect) {
    limitSelect.addEventListener('change', () => {
      filterState.limit = parseInt(limitSelect.value, 10) || 20;
      filterState.page = 1;
      performSearch();
    });
  }

  // Pagination navigation buttons
  document.getElementById('btn-page-first')?.addEventListener('click', () => {
    if (filterState.page > 1) {
      goToPage(1);
    }
  });

  document.getElementById('btn-page-prev')?.addEventListener('click', () => {
    if (filterState.page > 1) {
      goToPage(filterState.page - 1);
    }
  });

  document.getElementById('btn-page-next')?.addEventListener('click', () => {
    if (filterState.page < lastTotalPages) {
      goToPage(filterState.page + 1);
    }
  });

  document.getElementById('btn-page-last')?.addEventListener('click', () => {
    if (filterState.page < lastTotalPages) {
      goToPage(lastTotalPages);
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

function setCategoryFilter(category) {
  filterState.category = category;
  document.querySelectorAll('#category-pills .pill-btn').forEach(btn => {
    if (btn.getAttribute('data-category') === category) {
      btn.classList.add('active');
    } else {
      btn.classList.remove('active');
    }
  });
  performSearch();
}

function setSizePreset(preset) {
  filterState.sizePreset = preset;
  switch (preset) {
    case 'tiny':
      filterState.minSize = 0;
      filterState.maxSize = 500 * 1024 * 1024;
      break;
    case 'small':
      filterState.minSize = 500 * 1024 * 1024;
      filterState.maxSize = 2 * 1024 * 1024 * 1024;
      break;
    case 'medium':
      filterState.minSize = 2 * 1024 * 1024 * 1024;
      filterState.maxSize = 10 * 1024 * 1024 * 1024;
      break;
    case 'large':
      filterState.minSize = 10 * 1024 * 1024 * 1024;
      filterState.maxSize = 0;
      break;
    default:
      filterState.minSize = 0;
      filterState.maxSize = 0;
      break;
  }
}

function handleHeaderSort(key) {
  const current = filterState.sort;
  if (key === 'created') {
    filterState.sort = current === 'created_desc' ? 'created_asc' : 'created_desc';
  } else if (key === 'size') {
    filterState.sort = current === 'size_desc' ? 'size_asc' : 'size_desc';
  } else if (key === 'name') {
    filterState.sort = current === 'name_asc' ? 'name_desc' : 'name_asc';
  } else if (key === 'category') {
    filterState.sort = current === 'name_asc' ? 'name_desc' : 'name_asc';
  } else if (key === 'source') {
    filterState.sort = current === 'seeders_desc' ? 'created_desc' : 'seeders_desc';
  }
  updateSortIndicators();
  performSearch();
}

function updateSortIndicators() {
  const sort = filterState.sort;
  const sortSelect = document.getElementById('sort-select');
  if (sortSelect && sortSelect.value !== sort) {
    sortSelect.value = sort;
  }

  const icons = {
    name: document.getElementById('sort-icon-name'),
    category: document.getElementById('sort-icon-category'),
    size: document.getElementById('sort-icon-size'),
    source: document.getElementById('sort-icon-source'),
    created: document.getElementById('sort-icon-created'),
  };

  Object.keys(icons).forEach(k => {
    const icon = icons[k];
    if (!icon) return;
    icon.classList.remove('active');
    icon.textContent = '↕';
  });

  if (sort === 'created_desc' && icons.created) {
    icons.created.classList.add('active');
    icons.created.textContent = '▼';
  } else if (sort === 'created_asc' && icons.created) {
    icons.created.classList.add('active');
    icons.created.textContent = '▲';
  } else if (sort === 'size_desc' && icons.size) {
    icons.size.classList.add('active');
    icons.size.textContent = '▼';
  } else if (sort === 'size_asc' && icons.size) {
    icons.size.classList.add('active');
    icons.size.textContent = '▲';
  } else if (sort === 'name_asc' && icons.name) {
    icons.name.classList.add('active');
    icons.name.textContent = '▲';
  } else if (sort === 'name_desc' && icons.name) {
    icons.name.classList.add('active');
    icons.name.textContent = '▼';
  }
}

function resetFilters() {
  filterState.query = '';
  filterState.category = 'all';
  filterState.source = 'all';
  filterState.sort = 'created_desc';
  filterState.page = 1;
  filterState.limit = 20;
  setSizePreset('all');

  document.getElementById('search-input').value = '';
  const filterSource = document.getElementById('filter-source');
  if (filterSource) filterSource.value = 'all';
  const filterSize = document.getElementById('filter-size');
  if (filterSize) filterSize.value = 'all';
  const sortSelect = document.getElementById('sort-select');
  if (sortSelect) sortSelect.value = 'created_desc';
  const limitSelect = document.getElementById('select-page-limit');
  if (limitSelect) limitSelect.value = '20';

  document.querySelectorAll('#category-pills .pill-btn').forEach(btn => {
    btn.classList.toggle('active', btn.getAttribute('data-category') === 'all');
  });

  updateSortIndicators();
  performSearch();
}

function renderActiveFilterBadges() {
  const container = document.getElementById('active-filter-badges');
  if (!container) return;
  container.innerHTML = '';

  if (filterState.query.trim()) {
    createBadge(container, `Search: "${filterState.query.trim()}"`, () => {
      document.getElementById('search-input').value = '';
      filterState.query = '';
      performSearch();
    });
  }

  if (filterState.category !== 'all') {
    createBadge(container, `Category: ${filterState.category}`, () => {
      setCategoryFilter('all');
    });
  }

  if (filterState.source !== 'all') {
    createBadge(container, `Source: ${filterState.source === 'dht' ? 'DHT Wire' : 'Prowlarr'}`, () => {
      document.getElementById('filter-source').value = 'all';
      filterState.source = 'all';
      performSearch();
    });
  }

  if (filterState.sizePreset !== 'all') {
    const labels = {
      tiny: '< 500 MB',
      small: '500 MB – 2 GB',
      medium: '2 GB – 10 GB',
      large: '> 10 GB'
    };
    createBadge(container, `Size: ${labels[filterState.sizePreset] || filterState.sizePreset}`, () => {
      document.getElementById('filter-size').value = 'all';
      setSizePreset('all');
      performSearch();
    });
  }
}

function createBadge(container, labelText, onRemove) {
  const badge = document.createElement('span');
  badge.className = 'active-filter-badge';
  badge.innerHTML = `<span>${escapeHtml(labelText)}</span><button title="Remove filter">&times;</button>`;
  badge.querySelector('button').addEventListener('click', (e) => {
    e.stopPropagation();
    onRemove();
  });
  container.appendChild(badge);
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

    const dbSizeEl = document.getElementById('stat-db-size');
    if (dbSizeEl && data.db.db_size_bytes !== undefined) {
      dbSizeEl.textContent = formatBytes(data.db.db_size_bytes);
    }
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

async function performSearch(overrideQuery) {
  const loading = document.getElementById('loading-spinner');
  const countText = document.getElementById('results-count-text');
  const tbody = document.getElementById('torrents-tbody');
  const emptyState = document.getElementById('empty-state');
  const checkProwlarr = document.getElementById('check-prowlarr-live');
  const queryProwlarr = checkProwlarr ? checkProwlarr.checked : false;

  if (overrideQuery !== undefined) {
    filterState.query = overrideQuery;
  }

  if (loading) loading.style.display = 'inline-block';
  renderActiveFilterBadges();

  try {
    const params = new URLSearchParams({
      q: filterState.query,
      category: filterState.category !== 'all' ? filterState.category : '',
      source: filterState.source !== 'all' ? filterState.source : '',
      sort: filterState.sort,
      min_size: filterState.minSize > 0 ? filterState.minSize.toString() : '',
      max_size: filterState.maxSize > 0 ? filterState.maxSize.toString() : '',
      page: filterState.page.toString(),
      limit: filterState.limit.toString(),
      offset: ((filterState.page - 1) * filterState.limit).toString(),
      prowlarr: queryProwlarr ? 'true' : 'false'
    });

    const res = await fetch(`/api/v1/search?${params.toString()}`);
    const data = await res.json();

    currentTorrents = data.items || [];
    renderTable(currentTorrents);

    const total = data.total || 0;
    const limit = filterState.limit;
    const page = filterState.page;
    const totalPages = data.total_pages || Math.max(1, Math.ceil(total / limit));
    lastTotalPages = totalPages;

    if (countText) {
      if (filterState.query.trim() === '') {
        countText.textContent = `Showing ${currentTorrents.length} of ${total.toLocaleString()} torrents (Page ${page} of ${totalPages})`;
      } else {
        countText.textContent = `Found ${total.toLocaleString()} results for "${filterState.query}" (Page ${page} of ${totalPages})`;
      }
    }

    if (emptyState) {
      emptyState.style.display = currentTorrents.length === 0 ? 'block' : 'none';
    }

    renderPagination(total, page, limit, totalPages);
  } catch (err) {
    console.error('Search error:', err);
    if (countText) countText.textContent = 'Error fetching search results';
  } finally {
    if (loading) loading.style.display = 'none';
  }
}

function goToPage(p) {
  if (p < 1 || (lastTotalPages && p > lastTotalPages)) return;
  filterState.page = p;
  filterState.offset = (p - 1) * filterState.limit;
  performSearch();
  const tableWrap = document.querySelector('.torrent-table-wrap');
  if (tableWrap) {
    tableWrap.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
}

function renderPagination(total, page, limit, totalPages) {
  const paginationBar = document.getElementById('pagination-bar');
  if (!paginationBar) return;

  if (total <= 0) {
    paginationBar.style.display = 'none';
    return;
  }
  paginationBar.style.display = 'flex';

  const start = (page - 1) * limit + 1;
  const end = Math.min(page * limit, total);

  const startEl = document.getElementById('page-start');
  const endEl = document.getElementById('page-end');
  const totalEl = document.getElementById('page-total');
  if (startEl) startEl.textContent = start.toLocaleString();
  if (endEl) endEl.textContent = end.toLocaleString();
  if (totalEl) totalEl.textContent = total.toLocaleString();

  const btnFirst = document.getElementById('btn-page-first');
  const btnPrev = document.getElementById('btn-page-prev');
  const btnNext = document.getElementById('btn-page-next');
  const btnLast = document.getElementById('btn-page-last');

  if (btnFirst) btnFirst.disabled = page <= 1;
  if (btnPrev) btnPrev.disabled = page <= 1;
  if (btnNext) btnNext.disabled = page >= totalPages;
  if (btnLast) btnLast.disabled = page >= totalPages;

  const pageNums = document.getElementById('page-numbers');
  if (pageNums) {
    pageNums.innerHTML = '';
    const maxButtons = 5;
    let startPage = Math.max(1, page - 2);
    let endPage = Math.min(totalPages, startPage + maxButtons - 1);
    if (endPage - startPage < maxButtons - 1) {
      startPage = Math.max(1, endPage - maxButtons + 1);
    }

    if (startPage > 1) {
      addPageBtn(pageNums, 1, page === 1);
      if (startPage > 2) {
        addEllipsis(pageNums);
      }
    }

    for (let i = startPage; i <= endPage; i++) {
      addPageBtn(pageNums, i, page === i);
    }

    if (endPage < totalPages) {
      if (endPage < totalPages - 1) {
        addEllipsis(pageNums);
      }
      addPageBtn(pageNums, totalPages, page === totalPages);
    }
  }
}

function addPageBtn(container, pageNum, isActive) {
  const btn = document.createElement('button');
  btn.className = `btn-page ${isActive ? 'active' : ''}`;
  btn.textContent = pageNum;
  if (!isActive) {
    btn.addEventListener('click', () => goToPage(pageNum));
  }
  container.appendChild(btn);
}

function addEllipsis(container) {
  const span = document.createElement('span');
  span.className = 'page-ellipsis';
  span.textContent = '…';
  container.appendChild(span);
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

    const cat = item.category || 'Other';
    let catColor = '#94a3b8';
    let catBg = 'rgba(148, 163, 184, 0.15)';
    let catBorder = 'rgba(148, 163, 184, 0.3)';
    if (cat === 'Video') {
      catColor = '#c084fc';
      catBg = 'rgba(168, 85, 247, 0.15)';
      catBorder = 'rgba(168, 85, 247, 0.3)';
    } else if (cat === 'Audio') {
      catColor = '#f472b6';
      catBg = 'rgba(236, 72, 153, 0.15)';
      catBorder = 'rgba(236, 72, 153, 0.3)';
    } else if (cat === 'Software') {
      catColor = '#facc15';
      catBg = 'rgba(234, 179, 8, 0.15)';
      catBorder = 'rgba(234, 179, 8, 0.3)';
    } else if (cat === 'Books') {
      catColor = '#34d399';
      catBg = 'rgba(16, 185, 129, 0.15)';
      catBorder = 'rgba(16, 185, 129, 0.3)';
    }
    const catBadge = `<span class="badge" style="color: ${catColor}; background: ${catBg}; border: 1px solid ${catBorder}; cursor: pointer;" onclick="event.stopPropagation(); setCategoryFilter('${escapeHtml(cat)}')" title="Filter by ${escapeHtml(cat)}">${escapeHtml(cat)}</span>`;

    tr.innerHTML = `
      <td class="torrent-title-cell">
        <a href="javascript:void(0)" class="torrent-title" onclick="showDetails(${index})">${escapeHtml(item.name)}</a>
        <div class="torrent-hash">${item.info_hash}</div>
      </td>
      <td>${catBadge}</td>
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

