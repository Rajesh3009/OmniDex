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
    document.getElementById('stat-active-resolving').textContent = data.dht.active_resolving;
    document.getElementById('stat-queue-len').textContent = `Resolving wire peers / ${data.dht.queue_len} queued`;
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
