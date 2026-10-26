// OmniDex frontend application
let currentTorrents = [];
let lastTotalPages = 1;

const filterState = {
  query: '',
  category: 'all',
  source: 'all',
  resolution: 'all',
  codec: 'all',
  quality: 'all',
  releaseGroup: '',
  includeSuspicious: false,
  groupByTitle: true,
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
  initStatsToggle();
  loadStats();
  loadConfig();
  performSearch();
  checkUrlHash();

  // Auto-refresh metrics every 3 seconds
  setInterval(loadStats, 3000);

  window.addEventListener('hashchange', checkUrlHash);
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

  // Clicking on OmniDex brand redirects to clean homepage
  const brandHome = document.getElementById('brand-home-btn');
  if (brandHome) {
    const returnHome = () => {
      if (window.location.hash) {
        history.pushState(null, '', window.location.pathname);
      }
      document.querySelectorAll('.modal-overlay.active').forEach(m => m.classList.remove('active'));
      resetFilters();
      window.scrollTo({ top: 0, behavior: 'smooth' });
    };
    brandHome.addEventListener('click', returnHome);
    brandHome.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') returnHome();
    });
  }

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

  // Resolution selector
  const resSelect = document.getElementById('filter-res');
  if (resSelect) {
    resSelect.addEventListener('change', () => {
      filterState.resolution = resSelect.value;
      filterState.page = 1;
      performSearch();
    });
  }

  // Codec selector
  const codecSelect = document.getElementById('filter-codec');
  if (codecSelect) {
    codecSelect.addEventListener('change', () => {
      filterState.codec = codecSelect.value;
      filterState.page = 1;
      performSearch();
    });
  }

  // Quality selector
  const qualSelect = document.getElementById('filter-quality');
  if (qualSelect) {
    qualSelect.addEventListener('change', () => {
      filterState.quality = qualSelect.value;
      filterState.page = 1;
      performSearch();
    });
  }

  // Suspicious checkbox
  const checkSuspicious = document.getElementById('check-include-suspicious');
  if (checkSuspicious) {
    checkSuspicious.addEventListener('change', () => {
      filterState.includeSuspicious = checkSuspicious.checked;
      filterState.page = 1;
      performSearch();
    });
  }

  // Smart Grouping checkbox
  const checkGrouping = document.getElementById('check-group-releases');
  if (checkGrouping) {
    checkGrouping.addEventListener('change', () => {
      filterState.groupByTitle = checkGrouping.checked;
      renderTable(currentTorrents);
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

  document.getElementById('btn-clean-dead')?.addEventListener('click', () => {
    openModal('modal-prune-dead');
    updatePrunePreview();
  });

  document.getElementById('btn-quick-free-ram')?.addEventListener('click', (e) => {
    e.stopPropagation();
    triggerMemoryShrink();
  });

  document.getElementById('btn-janitor-shrink')?.addEventListener('click', () => {
    triggerMemoryShrink();
  });

  document.getElementById('btn-show-feed')?.addEventListener('click', () => {
    const origin = window.location.origin;
    const rssEl = document.getElementById('feed-rss-url');
    if (rssEl) rssEl.textContent = `${origin}/api/v1/feed/rss`;
    const atomEl = document.getElementById('feed-atom-url');
    if (atomEl) atomEl.textContent = `${origin}/api/v1/feed/atom`;
    openModal('modal-feed');
  });

  document.getElementById('btn-show-torznab').addEventListener('click', () => {
    openModal('modal-torznab');
  });

  document.getElementById('btn-show-settings').addEventListener('click', () => {
    openModal('modal-settings');
    loadFeedsList();
  });

  // Settings Tabs
  document.querySelectorAll('.settings-tabs .tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.settings-tabs .tab-btn').forEach(b => b.classList.remove('active'));
      document.querySelectorAll('.tab-panel').forEach(p => p.style.display = 'none');
      btn.classList.add('active');
      const targetId = btn.getAttribute('data-tab');
      const panel = document.getElementById(targetId);
      if (panel) panel.style.display = 'block';
      if (targetId === 'tab-feeds') {
        loadFeedsList();
      }
    });
  });

  // Clean Dead Torrents Form & Preview
  ['prune-days-select', 'prune-source-select', 'prune-include-spam'].forEach(id => {
    document.getElementById(id)?.addEventListener('change', updatePrunePreview);
  });

  let isPruningDead = false;
  async function handleExecutePrune(e) {
    if (e) e.preventDefault();
    if (isPruningDead) return;

    const btn = document.getElementById('btn-execute-prune');
    const msg = document.getElementById('prune-status-msg');
    const daysEl = document.getElementById('prune-days-select');
    const sourceEl = document.getElementById('prune-source-select');
    const spamEl = document.getElementById('prune-include-spam');

    const days = daysEl ? parseInt(daysEl.value, 10) : 14;
    const source = sourceEl ? sourceEl.value : 'all';
    const pruneSpam = spamEl ? spamEl.checked : false;

    isPruningDead = true;
    if (btn) {
      btn.disabled = true;
      btn.dataset.originalText = btn.innerHTML;
      btn.innerHTML = '⏳ Purging Torrents...';
    }
    if (msg) {
      msg.style.display = 'block';
      msg.style.background = 'rgba(6, 182, 212, 0.15)';
      msg.style.color = 'var(--accent-cyan)';
      msg.textContent = 'Purging dead torrents and optimizing SQLite database...';
    }

    try {
      const res = await fetch('/api/v1/torrents/prune', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ days, max_seeders: 0, source, prune_spam: pruneSpam })
      });
      const data = await res.json();
      if (res.ok) {
        if (msg) {
          msg.style.background = 'rgba(16, 185, 129, 0.15)';
          msg.style.color = '#34d399';
          msg.textContent = `✓ Successfully deleted ${data.deleted} dead torrent${data.deleted === 1 ? '' : 's'}!`;
        }
        updatePrunePreview();
        performSearch(undefined, false);
        loadStats();
        setTimeout(() => {
          closeModal('modal-prune-dead');
          if (msg) msg.style.display = 'none';
        }, 1500);
      } else {
        throw new Error(data.error || 'Failed to prune torrents');
      }
    } catch (err) {
      if (msg) {
        msg.style.background = 'rgba(239, 68, 68, 0.15)';
        msg.style.color = '#f87171';
        msg.textContent = 'Error: ' + err.message;
      }
    } finally {
      isPruningDead = false;
      if (btn) {
        btn.disabled = false;
        btn.innerHTML = btn.dataset.originalText || '🗑️ Purge Matching Torrents';
      }
    }
  }

  document.getElementById('prune-dead-form')?.addEventListener('submit', handleExecutePrune);
  document.getElementById('btn-execute-prune')?.addEventListener('click', handleExecutePrune);

  // Direct Torrent Import
  document.getElementById('btn-execute-import')?.addEventListener('click', async () => {
    const inputEl = document.getElementById('import-input');
    const nameEl = document.getElementById('import-name');
    const msg = document.getElementById('import-status-msg');
    const inputVal = inputEl?.value.trim();
    if (!inputVal) {
      alert('Please enter a magnet link or 40-character infohash');
      return;
    }

    if (msg) {
      msg.style.display = 'block';
      msg.style.background = 'rgba(6, 182, 212, 0.15)';
      msg.style.color = 'var(--accent-cyan)';
      msg.textContent = 'Importing torrent...';
    }

    try {
      const res = await fetch('/api/v1/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ input: inputVal, name: nameEl?.value.trim() })
      });
      const data = await res.json();
      if (res.ok) {
        msg.style.background = 'rgba(16, 185, 129, 0.15)';
        msg.style.color = '#34d399';
        msg.textContent = `✓ Imported: ${data.torrent.name}`;
        if (inputEl) inputEl.value = '';
        if (nameEl) nameEl.value = '';
        performSearch(undefined, false);
        loadStats();
      } else {
        throw new Error(data.error || 'Import failed');
      }
    } catch (err) {
      if (msg) {
        msg.style.background = 'rgba(239, 68, 68, 0.15)';
        msg.style.color = '#f87171';
        msg.textContent = 'Error: ' + err.message;
      }
    }
  });

  // Feeds Management Listeners
  document.getElementById('btn-sync-all-feeds')?.addEventListener('click', async () => {
    const btn = document.getElementById('btn-sync-all-feeds');
    if (btn) btn.textContent = '🔄 Syncing...';
    try {
      const res = await fetch('/api/v1/feeds/sync', { method: 'POST' });
      const data = await res.json();
      alert(`Sync finished! Added ${data.added} new torrents.`);
      loadFeedsList();
      performSearch(undefined, false);
      loadStats();
    } catch (e) {
      alert('Sync failed: ' + e.message);
    } finally {
      if (btn) btn.textContent = '🔄 Sync All Feeds Now';
    }
  });

  document.getElementById('btn-add-feed')?.addEventListener('click', async () => {
    const name = document.getElementById('new-feed-name')?.value.trim();
    const type = document.getElementById('new-feed-type')?.value;
    const url = document.getElementById('new-feed-url')?.value.trim();
    const apiKey = document.getElementById('new-feed-apikey')?.value.trim();
    const interval = parseInt(document.getElementById('new-feed-interval')?.value, 10) || 30;

    if (!name || !url) {
      alert('Please provide both Feed Name and Feed URL');
      return;
    }

    try {
      const res = await fetch('/api/v1/feeds', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, type, url, api_key: apiKey, interval_minutes: interval, enabled: true })
      });
      if (res.ok) {
        document.getElementById('new-feed-name').value = '';
        document.getElementById('new-feed-url').value = '';
        document.getElementById('new-feed-apikey').value = '';
        loadFeedsList();
      } else {
        alert('Failed to add feed');
      }
    } catch (e) {
      alert('Error: ' + e.message);
    }
  });

  // Single Torrent Details Delete Button
  document.getElementById('btn-delete-torrent')?.addEventListener('click', () => {
    if (currentDetailTorrent) {
      deleteTorrentPrompt(currentDetailTorrent.info_hash, currentDetailTorrent.name);
    }
  });

  document.querySelectorAll('[data-close]').forEach(btn => {
    btn.addEventListener('click', () => {
      const modalId = btn.getAttribute('data-close');
      closeModal(modalId);
    });
  });

  document.querySelectorAll('.modal-overlay').forEach(overlay => {
    overlay.addEventListener('click', (e) => {
      if (e.target === overlay) {
        closeModal(overlay.id);
      }
    });
  });

  // Settings form submission
  document.getElementById('settings-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const dlKb = parseFloat(document.getElementById('cfg-max-download-rate')?.value) || 0;
    const ulKb = parseFloat(document.getElementById('cfg-max-upload-rate')?.value) || 0;
    const monthlyGb = parseFloat(document.getElementById('cfg-monthly-data-limit')?.value) || 0;

    const payload = {
      prowlarr_url: document.getElementById('cfg-prowlarr-url').value,
      prowlarr_api_key: document.getElementById('cfg-prowlarr-key').value,
      torznab_api_key: document.getElementById('cfg-torznab-key').value,
      dht_workers: parseInt(document.getElementById('cfg-dht-workers').value, 10) || 15,
      max_download_rate: Math.max(0, Math.round(dlKb * 1024)),
      max_upload_rate: Math.max(0, Math.round(ulKb * 1024)),
      monthly_data_limit_bytes: Math.max(0, Math.round(monthlyGb * 1024 * 1024 * 1024)),
      auto_prune_dead: document.getElementById('cfg-auto-prune-dead')?.checked || false,
      auto_prune_days: parseInt(document.getElementById('cfg-auto-prune-days')?.value, 10) || 30
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
        loadStats();
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

function filterByTag(tagType, val) {
  filterState.page = 1;
  if (tagType === 'resolution') {
    filterState.resolution = val;
    const el = document.getElementById('filter-res');
    if (el) el.value = val;
  } else if (tagType === 'codec') {
    filterState.codec = val;
    const el = document.getElementById('filter-codec');
    if (el) el.value = val;
  } else if (tagType === 'quality') {
    filterState.quality = val;
    const el = document.getElementById('filter-quality');
    if (el) el.value = val;
  } else if (tagType === 'releaseGroup') {
    filterState.releaseGroup = val;
  }
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
  } else if (key === 'seeders') {
    filterState.sort = current === 'seeders_desc' ? 'seeders_asc' : 'seeders_desc';
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
    seeders: document.getElementById('sort-icon-seeders'),
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
  } else if (sort === 'seeders_desc' && icons.seeders) {
    icons.seeders.classList.add('active');
    icons.seeders.textContent = '▼';
  } else if (sort === 'seeders_asc' && icons.seeders) {
    icons.seeders.classList.add('active');
    icons.seeders.textContent = '▲';
  }
}

function resetFilters() {
  filterState.query = '';
  filterState.category = 'all';
  filterState.source = 'all';
  filterState.resolution = 'all';
  filterState.codec = 'all';
  filterState.quality = 'all';
  filterState.releaseGroup = '';
  filterState.includeSuspicious = false;
  filterState.sort = 'created_desc';
  filterState.page = 1;
  filterState.limit = 20;
  setSizePreset('all');

  document.getElementById('search-input').value = '';
  const filterSource = document.getElementById('filter-source');
  if (filterSource) filterSource.value = 'all';
  const filterSize = document.getElementById('filter-size');
  if (filterSize) filterSize.value = 'all';
  const filterRes = document.getElementById('filter-res');
  if (filterRes) filterRes.value = 'all';
  const filterCodec = document.getElementById('filter-codec');
  if (filterCodec) filterCodec.value = 'all';
  const filterQual = document.getElementById('filter-quality');
  if (filterQual) filterQual.value = 'all';
  const checkSuspicious = document.getElementById('check-include-suspicious');
  if (checkSuspicious) checkSuspicious.checked = false;
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

  if (filterState.includeSuspicious) {
    createBadge(container, `⚠️ Showing Suspicious`, () => {
      const el = document.getElementById('check-include-suspicious');
      if (el) el.checked = false;
      filterState.includeSuspicious = false;
      performSearch();
    });
  }

  if (filterState.resolution !== 'all') {
    createBadge(container, `Res: ${filterState.resolution}`, () => {
      const el = document.getElementById('filter-res');
      if (el) el.value = 'all';
      filterState.resolution = 'all';
      performSearch();
    });
  }

  if (filterState.codec !== 'all') {
    createBadge(container, `Codec: ${filterState.codec}`, () => {
      const el = document.getElementById('filter-codec');
      if (el) el.value = 'all';
      filterState.codec = 'all';
      performSearch();
    });
  }

  if (filterState.quality !== 'all') {
    createBadge(container, `Quality: ${filterState.quality}`, () => {
      const el = document.getElementById('filter-quality');
      if (el) el.value = 'all';
      filterState.quality = 'all';
      performSearch();
    });
  }

  if (filterState.releaseGroup) {
    createBadge(container, `Group: ${filterState.releaseGroup}`, () => {
      filterState.releaseGroup = '';
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
  const modal = document.getElementById(id);
  if (modal) modal.classList.remove('active');
  if (id === 'modal-details' && window.location.hash) {
    history.replaceState(null, '', window.location.pathname + window.location.search);
  }
}

let lastKnownTorrentCount = null;

async function loadStats() {
  try {
    const res = await fetch('/api/v1/stats');
    if (!res.ok) return;
    const data = await res.json();

    const totalTorrents = data.db.total_torrents;
    document.getElementById('stat-total-torrents').textContent = totalTorrents.toLocaleString();
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

    const ramEl = document.getElementById('stat-ram-usage');
    const ramSubEl = document.getElementById('stat-ram-sub');
    if (ramEl && data.memory) {
      const mem = data.memory;
      const displayRam = mem.rss_bytes || mem.alloc_bytes;
      ramEl.textContent = formatBytes(displayRam);

      const minRam = mem.min_rss_bytes || displayRam;
      const maxRam = mem.max_rss_bytes || mem.peak_rss_bytes || displayRam;

      if (ramSubEl) {
        ramSubEl.textContent = `Min: ${formatBytes(minRam)} · Max: ${formatBytes(maxRam)}`;
        const card = ramEl.closest('.stat-card');
        if (card) {
          card.title = `Current RAM (Working Set): ${formatBytes(displayRam)}\nMinimum Observed: ${formatBytes(minRam)}\nMaximum / Peak: ${formatBytes(maxRam)}\nGo Heap Allocated: ${formatBytes(mem.alloc_bytes)}\nOS Virtual Sys: ${formatBytes(mem.sys_bytes)}\nActive Goroutines: ${mem.num_goroutine}\nGC Cycles: ${mem.num_gc}`;
        }
      }
    }

    // Bandwidth Download & Upload Stats
    const dlRateEl = document.getElementById('stat-dl-rate');
    const dlTotalEl = document.getElementById('stat-dl-total');
    const ulRateEl = document.getElementById('stat-ul-rate');
    const ulTotalEl = document.getElementById('stat-ul-total');

    if (data.dht) {
      const dhtMetrics = data.dht;
      const monthlyLimitActive = dhtMetrics.monthly_data_limit_bytes > 0;
      const monthlyLimitFormatted = monthlyLimitActive ? formatBytes(dhtMetrics.monthly_data_limit_bytes) : 'None';
      const monthlyUsageFormatted = formatBytes(dhtMetrics.monthly_total_used || 0);

      if (dlRateEl) {
        if (dhtMetrics.monthly_limit_exceeded) {
          dlRateEl.innerHTML = `<span style="color: #ef4444;" title="Monthly internet data limit reached! DHT metadata indexing paused.">PAUSED</span>`;
        } else {
          dlRateEl.textContent = `${formatBytes(dhtMetrics.download_bytes_rate || 0)}/s`;
        }
      }
      if (dlTotalEl) {
        const dlLimitText = dhtMetrics.max_download_rate > 0 ? `${formatBytes(dhtMetrics.max_download_rate)}/s` : 'None';
        if (monthlyLimitActive) {
          dlTotalEl.textContent = `Month: ${formatBytes(dhtMetrics.monthly_downloaded || 0)} / ${monthlyLimitFormatted}`;
        } else {
          dlTotalEl.textContent = `Total: ${formatBytes(dhtMetrics.total_downloaded || 0)} · Rate Limit: ${dlLimitText}`;
        }
        const dlCard = document.getElementById('stat-card-download');
        if (dlCard) {
          dlCard.title = `Live Download Rate: ${formatBytes(dhtMetrics.download_bytes_rate || 0)}/s\nSession Inbound: ${formatBytes(dhtMetrics.session_downloaded || 0)}\nThis Month (${dhtMetrics.current_month || 'Current'}): ${formatBytes(dhtMetrics.monthly_downloaded || 0)}\nAll-Time Persistent: ${formatBytes(dhtMetrics.total_downloaded || 0)}\nConfigured Rate Limit: ${dlLimitText}\nMonthly Cap: ${monthlyLimitFormatted} (Used: ${monthlyUsageFormatted})${dhtMetrics.monthly_limit_exceeded ? '\n⚠️ MONTHLY LIMIT REACHED - CRAWLER PAUSED' : ''}`;
        }
      }

      if (ulRateEl) {
        if (dhtMetrics.monthly_limit_exceeded) {
          ulRateEl.innerHTML = `<span style="color: #ef4444;" title="Monthly internet data limit reached! DHT metadata indexing paused.">PAUSED</span>`;
        } else {
          ulRateEl.textContent = `${formatBytes(dhtMetrics.upload_bytes_rate || 0)}/s`;
        }
      }
      if (ulTotalEl) {
        const ulLimitText = dhtMetrics.max_upload_rate > 0 ? `${formatBytes(dhtMetrics.max_upload_rate)}/s` : 'None';
        if (monthlyLimitActive) {
          ulTotalEl.textContent = `Month: ${formatBytes(dhtMetrics.monthly_uploaded || 0)} / ${monthlyLimitFormatted}`;
        } else {
          ulTotalEl.textContent = `Total: ${formatBytes(dhtMetrics.total_uploaded || 0)} · Rate Limit: ${ulLimitText}`;
        }
        const ulCard = document.getElementById('stat-card-upload');
        if (ulCard) {
          ulCard.title = `Live Upload Rate: ${formatBytes(dhtMetrics.upload_bytes_rate || 0)}/s\nSession Outbound: ${formatBytes(dhtMetrics.session_uploaded || 0)}\nThis Month (${dhtMetrics.current_month || 'Current'}): ${formatBytes(dhtMetrics.monthly_uploaded || 0)}\nAll-Time Persistent: ${formatBytes(dhtMetrics.total_uploaded || 0)}\nConfigured Rate Limit: ${ulLimitText}\nMonthly Cap: ${monthlyLimitFormatted} (Used: ${monthlyUsageFormatted})${dhtMetrics.monthly_limit_exceeded ? '\n⚠️ MONTHLY LIMIT REACHED - CRAWLER PAUSED' : ''}`;
        }
      }
    }

    // Dynamic list update: If new torrents have been indexed (via DHT or Prowlarr)
    // and user is viewing page 1 with no active modal or editing in progress, refresh page 1 silently!
    if (lastKnownTorrentCount !== null && totalTorrents !== lastKnownTorrentCount) {
      const isModalOpen = !!document.querySelector('.modal-overlay.active');
      const searchInput = document.getElementById('search-input');
      const isTyping = searchInput && document.activeElement === searchInput;
      if (filterState.page === 1 && !isModalOpen && !isTyping) {
        // Silently update page 1 list without disrupting user
        performSearch(undefined, false, true);
      }
    }
    lastKnownTorrentCount = totalTorrents;
  } catch (e) {
    console.error('Stats error:', e);
  }
}

function initStatsToggle() {
  const btnToggle = document.getElementById('btn-toggle-stats');
  const statsGrid = document.querySelector('.stats-grid');
  const toggleText = document.getElementById('toggle-stats-text');
  if (!btnToggle || !statsGrid) return;

  const isHiddenSaved = localStorage.getItem('hide_stats') === 'true';
  if (isHiddenSaved) {
    statsGrid.classList.add('stats-hidden');
    btnToggle.classList.add('is-hidden');
    if (toggleText) toggleText.textContent = 'Show Stats';
  }

  btnToggle.addEventListener('click', () => {
    const isHidden = statsGrid.classList.toggle('stats-hidden');
    btnToggle.classList.toggle('is-hidden', isHidden);
    if (toggleText) {
      toggleText.textContent = isHidden ? 'Show Stats' : 'Hide Stats';
    }
    localStorage.setItem('hide_stats', isHidden ? 'true' : 'false');
  });
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

    const dlRateInput = document.getElementById('cfg-max-download-rate');
    if (dlRateInput) {
      dlRateInput.value = cfg.max_download_rate ? Math.round(cfg.max_download_rate / 1024) : 0;
    }
    const ulRateInput = document.getElementById('cfg-max-upload-rate');
    if (ulRateInput) {
      ulRateInput.value = cfg.max_upload_rate ? Math.round(cfg.max_upload_rate / 1024) : 0;
    }
    const monthlyLimitInput = document.getElementById('cfg-monthly-data-limit');
    if (monthlyLimitInput) {
      monthlyLimitInput.value = cfg.monthly_data_limit_bytes ? (cfg.monthly_data_limit_bytes / (1024 * 1024 * 1024)).toFixed(1) : 0;
    }

    document.getElementById('torznab-key-display').textContent = cfg.torznab_api_key || 'N/A';
    document.getElementById('torznab-url-display').textContent = `${window.location.origin}/api/v1/torznab`;

    const autoPruneCheck = document.getElementById('cfg-auto-prune-dead');
    if (autoPruneCheck) {
      autoPruneCheck.checked = !!cfg.auto_prune_dead;
    }
    const autoPruneDays = document.getElementById('cfg-auto-prune-days');
    if (autoPruneDays && cfg.auto_prune_days) {
      autoPruneDays.value = cfg.auto_prune_days;
    }
  } catch (e) {
    console.error('Config fetch error:', e);
  }
}

let prowlarrSearchSeq = 0;
let prowlarrAbortCtrl = null;

async function triggerBackgroundProwlarrSearch(query) {
  const indicator = document.getElementById('prowlarr-live-indicator');
  if (!indicator) return;

  const currentSeq = ++prowlarrSearchSeq;
  if (prowlarrAbortCtrl) {
    try { prowlarrAbortCtrl.abort(); } catch (e) {}
  }
  prowlarrAbortCtrl = new AbortController();

  indicator.className = 'prowlarr-status-pill';
  indicator.textContent = '📡 Querying Prowlarr in background...';
  indicator.style.display = 'inline-flex';

  try {
    const res = await fetch(`/api/v1/prowlarr/search?q=${encodeURIComponent(query)}&limit=30`, {
      signal: prowlarrAbortCtrl.signal
    });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const data = await res.json();

    if (currentSeq !== prowlarrSearchSeq) return;

    if (data.status === 'unconfigured') {
      indicator.style.display = 'none';
      return;
    }

    if (data.added > 0) {
      indicator.className = 'prowlarr-status-pill success';
      indicator.textContent = `✓ Prowlarr added ${data.added} new torrent${data.added > 1 ? 's' : ''}`;
      // Refresh local results immediately to display newly ingested items in the table
      if (filterState.query.trim().toLowerCase() === query.trim().toLowerCase()) {
        performSearch(filterState.query, false);
      }
      setTimeout(() => {
        if (currentSeq === prowlarrSearchSeq) {
          indicator.style.display = 'none';
        }
      }, 4000);
    } else {
      indicator.className = 'prowlarr-status-pill dim';
      indicator.textContent = data.count > 0 ? `✓ Prowlarr checked (${data.count} already indexed)` : `✓ Prowlarr checked (no external results)`;
      if (data.count > 0 && filterState.query.trim().toLowerCase() === query.trim().toLowerCase()) {
        // In case items were indexed in background or previously, refresh results list
        performSearch(filterState.query, false);
      }
      setTimeout(() => {
        if (currentSeq === prowlarrSearchSeq) {
          indicator.style.display = 'none';
        }
      }, 2500);
    }
  } catch (err) {
    if (err.name === 'AbortError') return;
    if (currentSeq !== prowlarrSearchSeq) return;
    indicator.style.display = 'none';
  }
}

async function performSearch(overrideQuery, triggerProwlarr = true, silent = false) {
  const loading = document.getElementById('loading-spinner');
  const countText = document.getElementById('results-count-text');
  const tbody = document.getElementById('torrents-tbody');
  const emptyState = document.getElementById('empty-state');
  const checkProwlarr = document.getElementById('check-prowlarr-live');
  // Default to true when checkbox is absent — always query Prowlarr in the background
  const queryProwlarr = checkProwlarr ? checkProwlarr.checked : true;

  if (overrideQuery !== undefined) {
    filterState.query = overrideQuery;
  }

  if (loading && !silent) loading.style.display = 'inline-block';
  renderActiveFilterBadges();

  try {
    const params = new URLSearchParams({
      q: filterState.query,
      category: filterState.category !== 'all' ? filterState.category : '',
      source: filterState.source !== 'all' ? filterState.source : '',
      resolution: filterState.resolution !== 'all' ? filterState.resolution : '',
      codec: filterState.codec !== 'all' ? filterState.codec : '',
      quality: filterState.quality !== 'all' ? filterState.quality : '',
      release_group: filterState.releaseGroup || '',
      include_suspicious: filterState.includeSuspicious ? 'true' : 'false',
      sort: filterState.sort,
      min_size: filterState.minSize > 0 ? filterState.minSize.toString() : '',
      max_size: filterState.maxSize > 0 ? filterState.maxSize.toString() : '',
      page: filterState.page.toString(),
      limit: filterState.limit.toString(),
      offset: ((filterState.page - 1) * filterState.limit).toString(),
      prowlarr: 'false', // Local search is always non-blocking
      _t: Date.now().toString()
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

    // After instant local search finishes, if Prowlarr is enabled, query in background
    if (triggerProwlarr && queryProwlarr && filterState.query.trim() !== '' && filterState.page === 1) {
      triggerBackgroundProwlarrSearch(filterState.query.trim());
    }
  } catch (err) {
    console.error('Search error:', err);
    if (countText && !silent) countText.textContent = 'Error fetching search results';
  } finally {
    if (loading && !silent) loading.style.display = 'none';
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

function cleanTitleKey(name) {
  if (!name) return '';
  let s = name.toLowerCase();
  // Remove file extensions
  s = s.replace(/\.(mkv|mp4|avi|iso|zip|rar|7z|tar|gz|exe|epub|pdf)$/i, '');
  // Remove bracketed info [xxx] or (xxx)
  s = s.replace(/\[[^\]]*\]/g, ' ').replace(/\([^)]*\)/g, ' ');
  // Remove resolution, quality, codec, release info
  const removeTerms = [
    '2160p', '4k', 'uhd', '1080p', '1080i', '720p', '480p', '576p',
    'bluray', 'bdrip', 'brrip', 'remux', 'web-dl', 'webdl', 'webrip', 'web', 'hdtv', 'pdtv', 'cam', 'ts',
    'x265', 'h265', 'hevc', 'x264', 'h264', 'avc', 'av1', 'xvid', 'divx',
    '10bit', '8bit', 'hdr10+', 'hdr10', 'hdr', 'dovi', 'dv',
    'dts-hd', 'truehd', 'atmos', 'dts', 'dd5.1', 'ac3', 'eac3', 'aac', 'flac', 'mp3',
    'yts', 'yify', 'rarbg', 'tgx', 'psa', 'flux', 'ntb', 'galaxyrg'
  ];
  for (const t of removeTerms) {
    const reg = new RegExp(`\\b${t}\\b`, 'gi');
    s = s.replace(reg, ' ');
  }
  // Replace dots, underscores, dashes with spaces
  s = s.replace(/[-_.]+/g, ' ');
  return s.trim().replace(/\s+/g, ' ');
}

function renderTable(items) {
  const tbody = document.getElementById('torrents-tbody');
  tbody.innerHTML = '';

  if (!filterState.groupByTitle || items.length === 0) {
    items.forEach((item, index) => {
      renderTorrentRow(tbody, item, index, null);
    });
    return;
  }

  // Group items by cleaned title key
  const groups = new Map();
  items.forEach((item, index) => {
    const key = cleanTitleKey(item.name);
    if (!key || key.length < 3) {
      // If title cannot be meaningfully normalized, treat as individual group
      groups.set(item.info_hash, [{ item, index }]);
    } else {
      if (!groups.has(key)) {
        groups.set(key, []);
      }
      groups.get(key).push({ item, index });
    }
  });

  let groupCounter = 0;
  groups.forEach((groupItems) => {
    groupCounter++;
    const primary = groupItems[0];
    const subItems = groupItems.slice(1);
    const groupId = `grp-${groupCounter}`;

    renderTorrentRow(tbody, primary.item, primary.index, {
      groupId: groupId,
      totalReleases: groupItems.length,
      hasChildren: subItems.length > 0
    });

    if (subItems.length > 0) {
      subItems.forEach(sub => {
        renderTorrentRow(tbody, sub.item, sub.index, {
          groupId: groupId,
          isChild: true
        });
      });
    }
  });
}

function renderTorrentRow(tbody, item, index, groupMeta) {
  const tr = document.createElement('tr');
  tr.className = 'torrent-row';

  if (groupMeta && groupMeta.isChild) {
    tr.className += ` child-release-row child-of-${groupMeta.groupId}`;
    tr.style.display = 'none'; // Initially collapsed
  }

  let sourceBadge = '<span class="badge badge-dht">DHT Wire</span>';
  if (item.source === 'prowlarr') {
    sourceBadge = '<span class="badge badge-prowlarr">Prowlarr</span>';
  } else if (item.source === 'manual') {
    sourceBadge = '<span class="badge" style="color: #38bdf8; background: rgba(56, 189, 248, 0.15); border: 1px solid rgba(56, 189, 248, 0.3);">Manual</span>';
  } else if (item.source && item.source !== 'dht') {
    sourceBadge = `<span class="badge" style="color: #fb923c; background: rgba(251, 146, 60, 0.15); border: 1px solid rgba(251, 146, 60, 0.3);" title="Source: ${escapeHtml(item.source)}">${escapeHtml(item.source)}</span>`;
  }

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
  } else if (cat === 'XXX') {
    catColor = '#fb7185';
    catBg = 'rgba(244, 63, 94, 0.15)';
    catBorder = 'rgba(244, 63, 94, 0.35)';
  }
  const catBadge = `<span class="badge" style="color: ${catColor}; background: ${catBg}; border: 1px solid ${catBorder}; cursor: pointer;" onclick="event.stopPropagation(); setCategoryFilter('${escapeHtml(cat)}')" title="Filter by ${escapeHtml(cat)}">${escapeHtml(cat)}</span>`;

  let tagsHtml = '';
  const tagBadges = [];
  if (item.resolution) {
    const cls = `tag-res-${item.resolution.toLowerCase()}`;
    tagBadges.push(`<span class="tag-badge ${cls}" onclick="event.stopPropagation(); filterByTag('resolution', '${escapeHtml(item.resolution)}')">${escapeHtml(item.resolution)}</span>`);
  }
  if (item.codec) {
    tagBadges.push(`<span class="tag-badge tag-codec" onclick="event.stopPropagation(); filterByTag('codec', '${escapeHtml(item.codec)}')">${escapeHtml(item.codec)}</span>`);
  }
  if (item.quality) {
    tagBadges.push(`<span class="tag-badge tag-quality" onclick="event.stopPropagation(); filterByTag('quality', '${escapeHtml(item.quality)}')">${escapeHtml(item.quality)}</span>`);
  }
  if (item.hdr) {
    tagBadges.push(`<span class="tag-badge tag-hdr">${escapeHtml(item.hdr)}</span>`);
  }
  if (item.audio) {
    tagBadges.push(`<span class="tag-badge tag-audio">${escapeHtml(item.audio)}</span>`);
  }
  if (item.release_group) {
    tagBadges.push(`<span class="tag-badge tag-group" onclick="event.stopPropagation(); filterByTag('releaseGroup', '${escapeHtml(item.release_group)}')">${escapeHtml(item.release_group)}</span>`);
  }
  if (item.is_suspicious) {
    tagBadges.push(`<span class="tag-badge tag-spam" title="${escapeHtml(item.spam_reasons || 'Flagged by anti-spam heuristics')}">⚠️ Suspicious (${item.spam_score}%)</span>`);
  }
  if (tagBadges.length > 0) {
    tagsHtml = `<div class="tags-row">${tagBadges.join('')}</div>`;
  }

  // Release group badge toggle for parent row
  let groupBadgeHtml = '';
  if (groupMeta && groupMeta.hasChildren) {
    groupBadgeHtml = `
      <button class="btn-group-toggle" onclick="event.stopPropagation(); toggleReleaseGroup('${groupMeta.groupId}', this)">
        <span class="group-toggle-arrow">▶</span>
        <span>${groupMeta.totalReleases} releases</span>
      </button>
    `;
  } else if (groupMeta && groupMeta.isChild) {
    groupBadgeHtml = `<span class="child-indicator">↳</span>`;
  }

  tr.innerHTML = `
    <td class="torrent-title-cell" data-label="Title">
      <div style="display: flex; align-items: flex-start; gap: 6px;">
        ${groupBadgeHtml}
        <div style="flex: 1; min-width: 0;">
          <a href="javascript:void(0)" class="torrent-title" onclick="showDetails(${index})">${escapeHtml(item.name)}</a>
          ${tagsHtml}
          <div class="torrent-hash">${item.info_hash}</div>
        </div>
      </div>
    </td>
    <td data-label="Category">${catBadge}</td>
    <td data-label="Size">${formatBytes(item.size)}</td>
    <td data-label="Files">
      <button class="btn-files-badge" onclick="event.stopPropagation(); showFiles(${index})" title="View files in this torrent">
        📁 ${item.file_count || 1} ${item.file_count === 1 ? 'file' : 'files'}
      </button>
    </td>
    <td class="peers-cell" data-label="Peers">
      <div class="peer-badges">
        <span class="seeders-badge" title="${item.seeders || 0} Seeders">▲ ${item.seeders || 0}</span>
        <span class="leechers-badge" title="${item.leechers || 0} Leechers">▼ ${item.leechers || 0}</span>
      </div>
    </td>
    <td data-label="Source">${sourceBadge}</td>
    <td data-label="Added" style="color: var(--text-dim); font-size: 0.8rem;">${formatDate(item.created_time)}</td>
    <td class="torrent-actions-cell" data-label="Actions" style="text-align: right;">
      <div class="action-btns" style="justify-content: flex-end;">
        <a href="${magnet}" class="btn-icon" title="Magnet Download">🧲</a>
        <button class="btn-icon" onclick="showFiles(${index})" title="View Files & Details">📁</button>
        <button class="btn-icon btn-icon-delete" onclick="event.stopPropagation(); deleteTorrentPrompt('${item.info_hash}', '${escapeHtml(item.name).replace(/'/g, "\\'")}')" title="Delete from database">🗑️</button>
      </div>
    </td>
  `;
  tbody.appendChild(tr);
}

function toggleReleaseGroup(groupId, btn) {
  const childRows = document.querySelectorAll(`.child-of-${groupId}`);
  const isExpanded = btn.classList.toggle('expanded');
  const arrow = btn.querySelector('.group-toggle-arrow');
  if (arrow) {
    arrow.textContent = isExpanded ? '▼' : '▶';
  }
  childRows.forEach(row => {
    row.style.display = isExpanded ? 'table-row' : 'none';
  });
}

function getFileIcon(filename) {
  if (!filename) return '📄';
  const ext = filename.split('.').pop().toLowerCase();
  const video = ['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm', 'm4v', 'ts', 'vob', 'iso'];
  const audio = ['mp3', 'flac', 'wav', 'aac', 'ogg', 'm4a', 'wma', 'opus'];
  const subtitle = ['srt', 'sub', 'ass', 'vtt', 'idx', 'smi'];
  const archive = ['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'tgz'];
  const image = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg'];
  const doc = ['pdf', 'epub', 'mobi', 'azw3', 'txt', 'nfo', 'doc', 'docx', 'cbz', 'cbr'];
  const app = ['exe', 'msi', 'apk', 'dmg', 'pkg', 'deb', 'sh', 'bat'];

  if (video.includes(ext)) return '🎬';
  if (audio.includes(ext)) return '🎵';
  if (subtitle.includes(ext)) return '📝';
  if (archive.includes(ext)) return '📦';
  if (image.includes(ext)) return '🖼️';
  if (doc.includes(ext)) return '📚';
  if (app.includes(ext)) return '💻';
  return '📄';
}

let activeDetailFiles = [];
let currentDetailTorrent = null;

async function openTorrentDetails(item) {
  if (!item) return;
  currentDetailTorrent = item;

  document.getElementById('detail-title').textContent = item.name;
  document.getElementById('detail-hash').textContent = item.info_hash;

  // Metadata Overview Bar
  const sizeEl = document.getElementById('detail-meta-size');
  if (sizeEl) sizeEl.textContent = `💾 ${formatBytes(item.size)}`;

  const seedersEl = document.getElementById('detail-meta-seeders');
  if (seedersEl) seedersEl.textContent = `▲ ${item.seeders || 0} Seeders`;

  const leechersEl = document.getElementById('detail-meta-leechers');
  if (leechersEl) leechersEl.textContent = `▼ ${item.leechers || 0} Leechers`;

  const catEl = document.getElementById('detail-meta-category');
  if (catEl) catEl.textContent = `🏷️ ${item.category || 'Other'}`;

  const sourceEl = document.getElementById('detail-meta-source');
  if (sourceEl) sourceEl.textContent = `📡 ${item.source === 'prowlarr' ? 'Prowlarr' : 'DHT Wire'}`;

  const dateEl = document.getElementById('detail-meta-date');
  if (dateEl) dateEl.textContent = `🕒 ${formatDate(item.created_time)}`;

  // Populate detail tags
  const tagsRow = document.getElementById('detail-tags-row');
  if (tagsRow) {
    tagsRow.innerHTML = '';
    const badges = [];
    if (item.resolution) badges.push(`<span class="tag-badge tag-res-${item.resolution.toLowerCase()}">📺 ${escapeHtml(item.resolution)}</span>`);
    if (item.codec) badges.push(`<span class="tag-badge tag-codec">🎞️ ${escapeHtml(item.codec)}</span>`);
    if (item.quality) badges.push(`<span class="tag-badge tag-quality">✨ ${escapeHtml(item.quality)}</span>`);
    if (item.hdr) badges.push(`<span class="tag-badge tag-hdr">🌈 ${escapeHtml(item.hdr)}</span>`);
    if (item.audio) badges.push(`<span class="tag-badge tag-audio">🔊 ${escapeHtml(item.audio)}</span>`);
    if (item.release_group) badges.push(`<span class="tag-badge tag-group">👥 ${escapeHtml(item.release_group)}</span>`);
    if (item.is_suspicious) badges.push(`<span class="tag-badge tag-spam" title="${escapeHtml(item.spam_reasons || 'Flagged by anti-spam heuristics')}">⚠️ Suspicious Risk (${item.spam_score}%): ${escapeHtml(item.spam_reasons || 'Unverified content')}</span>`);
    tagsRow.innerHTML = badges.join('');
    tagsRow.style.display = badges.length > 0 ? 'flex' : 'none';
  }
  
  const magnet = item.magnet_uri || `magnet:?xt=urn:btih:${item.info_hash}&dn=${encodeURIComponent(item.name)}`;
  document.getElementById('detail-magnet').textContent = magnet;
  document.getElementById('detail-magnet-link').href = magnet;

  const btnCopyMagnet = document.getElementById('btn-copy-magnet');
  if (btnCopyMagnet) {
    btnCopyMagnet.onclick = () => {
      navigator.clipboard.writeText(magnet).then(() => {
        const orig = btnCopyMagnet.textContent;
        btnCopyMagnet.textContent = '✓ Copied!';
        setTimeout(() => btnCopyMagnet.textContent = orig, 1500);
      });
    };
  }

  // Parse files
  let rawFiles = [];
  try {
    if (item.files_json) {
      rawFiles = typeof item.files_json === 'string' ? JSON.parse(item.files_json) : item.files_json;
    }
  } catch (e) {}

  activeDetailFiles = normalizeFiles(rawFiles, item.name);
  renderFilesList(activeDetailFiles);

  const filterInput = document.getElementById('detail-files-filter');
  if (filterInput) {
    filterInput.value = '';
    filterInput.oninput = () => {
      const q = filterInput.value.trim().toLowerCase();
      if (!q) {
        renderFilesList(activeDetailFiles);
      } else {
        const filtered = activeDetailFiles.filter(f => f.path.toLowerCase().includes(q));
        renderFilesList(filtered);
      }
    };
  }

  const btnCopyFiles = document.getElementById('btn-copy-files');
  if (btnCopyFiles) {
    btnCopyFiles.onclick = () => {
      const text = activeDetailFiles.map(f => f.path).join('\n');
      navigator.clipboard.writeText(text).then(() => {
        const orig = btnCopyFiles.textContent;
        btnCopyFiles.textContent = '✓ Copied!';
        setTimeout(() => btnCopyFiles.textContent = orig, 1500);
      });
    };
  }

  openModal('modal-details');

  // If item had empty or missing files_json, fetch single record to populate
  if (!rawFiles || rawFiles.length === 0) {
    const loadingEl = document.getElementById('detail-files-loading');
    if (loadingEl) loadingEl.style.display = 'inline';
    try {
      const res = await fetch(`/api/v1/torrent?hash=${encodeURIComponent(item.info_hash)}`);
      if (res.ok) {
        const fullItem = await res.json();
        if (fullItem && fullItem.files_json) {
          const fetchedFiles = typeof fullItem.files_json === 'string' ? JSON.parse(fullItem.files_json) : fullItem.files_json;
          activeDetailFiles = normalizeFiles(fetchedFiles, item.name);
          renderFilesList(activeDetailFiles);
        }
      }
    } catch (e) {
      console.error('Failed to load files:', e);
    } finally {
      if (loadingEl) loadingEl.style.display = 'none';
    }
  }
}

function normalizeFiles(files, defaultName) {
  if (!files || files.length === 0) {
    return [{ path: defaultName || 'Unknown file', size: 0 }];
  }
  return files.map(f => {
    if (typeof f === 'string') {
      return { path: f, size: 0 };
    } else if (f && typeof f === 'object') {
      return { path: f.path || f.name || defaultName, size: f.size || 0 };
    }
    return { path: String(f), size: 0 };
  });
}

function renderFilesList(files) {
  const container = document.getElementById('detail-files-list');
  const countEl = document.getElementById('detail-files-count');
  if (countEl) countEl.textContent = files.length.toLocaleString();
  if (!container) return;

  container.innerHTML = '';
  if (files.length === 0) {
    container.innerHTML = '<div style="color: var(--text-dim); padding: 12px; text-align: center; font-size: 0.85rem;">No files matched the filter</div>';
    return;
  }

  files.forEach(f => {
    const row = document.createElement('div');
    row.className = 'file-row';
    const icon = getFileIcon(f.path);
    const sizeBadge = f.size > 0 ? `<span class="file-size-badge">${formatBytes(f.size)}</span>` : '';
    
    row.innerHTML = `
      <div class="file-row-left">
        <span class="file-icon">${icon}</span>
        <span class="file-path" title="${escapeHtml(f.path)}">${escapeHtml(f.path)}</span>
      </div>
      ${sizeBadge}
    `;
    container.appendChild(row);
  });
}

function showDetails(index) {
  const item = currentTorrents[index];
  if (item) {
    openTorrentDetails(item);
  }
}

function showFiles(index) {
  const item = currentTorrents[index];
  if (item) {
    openTorrentDetails(item);
    setTimeout(() => {
      const filterInput = document.getElementById('detail-files-filter');
      if (filterInput) filterInput.focus();
    }, 150);
  }
}

async function checkUrlHash() {
  const hash = (window.location.hash || '').replace(/^[#/]+/, '').trim();
  if (!hash || hash.length < 32) {
    return;
  }

  try {
    const res = await fetch(`/api/v1/torrent?hash=${encodeURIComponent(hash)}`);
    if (res.ok) {
      const item = await res.json();
      if (item && item.info_hash) {
        openTorrentDetails(item);
      }
    }
  } catch (e) {
    console.error('Error fetching torrent by hash:', e);
  }
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

async function deleteTorrentPrompt(hash, name) {
  if (!confirm(`Are you sure you want to permanently delete this torrent from the database and search index?\n\n"${name}"\n(${hash})`)) {
    return;
  }
  try {
    const res = await fetch(`/api/v1/torrent?hash=${encodeURIComponent(hash)}`, { method: 'DELETE' });
    if (res.ok) {
      closeModal('modal-details');
      performSearch(undefined, false);
      loadStats();
    } else {
      const err = await res.text();
      alert('Failed to delete torrent: ' + err);
    }
  } catch (e) {
    alert('Error deleting torrent: ' + e.message);
  }
}

async function triggerMemoryShrink() {
  const ramEl = document.getElementById('stat-ram-usage');
  const origText = ramEl ? ramEl.textContent : '';
  if (ramEl) ramEl.textContent = 'Freeing...';

  try {
    const res = await fetch('/api/v1/memory/shrink', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      loadStats();
      updateJanitorStats(data.memory);
    }
  } catch (e) {
    console.error('Memory shrink failed:', e);
    if (ramEl) ramEl.textContent = origText;
  }
}

function updateJanitorStats(mem) {
  if (!mem) return;
  const rssEl = document.getElementById('janitor-rss');
  const heapEl = document.getElementById('janitor-heap');
  const sysEl = document.getElementById('janitor-sys');
  const goroutinesEl = document.getElementById('janitor-goroutines');

  if (rssEl) rssEl.textContent = formatBytes(mem.rss_bytes || mem.alloc_bytes);
  if (heapEl) heapEl.textContent = formatBytes(mem.heap_inuse_bytes || mem.alloc_bytes);
  if (sysEl) sysEl.textContent = formatBytes(mem.sys_bytes);
  if (goroutinesEl) goroutinesEl.textContent = String(mem.num_goroutine);
}

async function updatePrunePreview() {
  const days = document.getElementById('prune-days-select')?.value || 14;
  const countEl = document.getElementById('prune-preview-count');
  if (countEl) countEl.textContent = 'Calculating...';

  try {
    const res = await fetch(`/api/v1/torrents/dead-count?days=${days}&max_seeders=0`);
    if (res.ok) {
      const data = await res.json();
      if (countEl) countEl.textContent = `${data.count.toLocaleString()} dead torrent${data.count === 1 ? '' : 's'}`;
    }
  } catch (e) {
    if (countEl) countEl.textContent = 'Error';
  }
}

async function loadFeedsList() {
  const container = document.getElementById('feeds-list-container');
  if (!container) return;

  try {
    const res = await fetch('/api/v1/feeds');
    if (!res.ok) return;
    const feeds = await res.json();

    if (feeds.length === 0) {
      container.innerHTML = `<div style="color: var(--text-dim); font-size: 0.85rem; padding: 12px; text-align: center;">No external feeds configured. Add one below!</div>`;
      return;
    }

    container.innerHTML = feeds.map(f => {
      const statusColor = f.enabled ? '#34d399' : '#94a3b8';
      const statusText = f.enabled ? 'Enabled' : 'Disabled';
      const syncInfo = f.last_sync && !f.last_sync.startsWith('0001') ? `Last: ${formatDate(f.last_sync)} (${f.last_count} added)` : 'Never synced';
      const errBadge = f.last_error ? `<span style="color: #f87171; font-size: 0.75rem;" title="${escapeHtml(f.last_error)}">⚠️ Error</span>` : '';

      return `
        <div class="feed-item-card">
          <div style="flex: 1; min-width: 0;">
            <div style="display: flex; align-items: center; gap: 8px;">
              <span style="font-weight: 600; font-size: 0.9rem;">${escapeHtml(f.name)}</span>
              <span class="badge" style="font-size: 0.7rem; color: ${statusColor}; border-color: ${statusColor};">${statusText}</span>
              <span class="badge" style="font-size: 0.7rem; color: var(--accent-cyan);">${escapeHtml(f.type.toUpperCase())}</span>
              ${errBadge}
            </div>
            <div style="font-size: 0.75rem; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 2px;">
              ${escapeHtml(f.url)} · ${syncInfo}
            </div>
          </div>
          <div style="display: flex; gap: 6px; align-items: center;">
            <button type="button" class="btn btn-secondary" style="padding: 4px 10px; font-size: 0.8rem;" onclick="syncSingleFeed('${f.id}')" title="Sync this feed now">🔄</button>
            <button type="button" class="btn btn-secondary" style="padding: 4px 10px; font-size: 0.8rem;" onclick="toggleFeed('${f.id}', ${!f.enabled})" title="${f.enabled ? 'Disable' : 'Enable'} feed">${f.enabled ? '⏸️' : '▶️'}</button>
            <button type="button" class="btn btn-secondary" style="padding: 4px 10px; font-size: 0.8rem; color: #f87171;" onclick="deleteFeed('${f.id}', '${escapeHtml(f.name)}')" title="Delete feed">🗑️</button>
          </div>
        </div>
      `;
    }).join('');
  } catch (e) {
    container.innerHTML = `<div style="color: #f87171; font-size: 0.85rem; padding: 10px;">Failed to load feeds: ${e.message}</div>`;
  }
}

async function syncSingleFeed(id) {
  try {
    const res = await fetch(`/api/v1/feeds/sync?id=${encodeURIComponent(id)}`, { method: 'POST' });
    const data = await res.json();
    if (res.ok) {
      alert(`Sync finished! Added ${data.added} new torrents.`);
      loadFeedsList();
      performSearch(undefined, false);
      loadStats();
    } else {
      alert('Sync failed: ' + (data.error || 'Unknown error'));
    }
  } catch (e) {
    alert('Sync error: ' + e.message);
  }
}

async function toggleFeed(id, enabled) {
  try {
    const res = await fetch('/api/v1/feeds');
    const feeds = await res.json();
    const feed = feeds.find(f => f.id === id);
    if (!feed) return;
    feed.enabled = enabled;
    await fetch('/api/v1/feeds', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(feed)
    });
    loadFeedsList();
  } catch (e) {
    console.error('Toggle error:', e);
  }
}

async function deleteFeed(id, name) {
  if (!confirm(`Delete feed "${name}"?`)) return;
  try {
    await fetch(`/api/v1/feeds?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
    loadFeedsList();
  } catch (e) {
    console.error('Delete feed error:', e);
  }
}

