// OmniDex Dedicated Settings Controller

let pendingDeleteFeedId = null;
let filterKwTimer = null;

document.addEventListener('DOMContentLoaded', () => {
  initTabs();
  initFormListeners();
  initKeyMasks();
  initFeeds();
  initDirectImport();
  initRulesFilter();
  initJanitor();
  initKeyboardShortcuts();
  initTrendingHarvester();
  initDownloadClients();
  
  loadConfig();
  loadFeedsList();
  loadScraperStats();
  loadJanitorStats();
  loadTrendingStatus();
  loadDownloadClients();

  // Periodically refresh stats
  setInterval(loadScraperStats, 10000);
  setInterval(loadJanitorStats, 5000);
  setInterval(loadTrendingStatus, 15000);
});

// Tab Switching & Hash Deep Linking
function initTabs() {
  const navItems = document.querySelectorAll('.settings-nav-item');
  const sections = document.querySelectorAll('.settings-section-card');

  function switchTab(tabId, updateHash = true) {
    const targetSection = document.getElementById(tabId);
    if (!targetSection) return;

    navItems.forEach(item => {
      if (item.getAttribute('data-tab') === tabId) {
        item.classList.add('active');
      } else {
        item.classList.remove('active');
      }
    });

    sections.forEach(sec => sec.classList.remove('active'));
    targetSection.classList.add('active');

    if (updateHash) {
      const hashName = tabId.replace('tab-', '');
      history.replaceState(null, '', `#${hashName}`);
    }

    // Refresh context-specific data when tab is opened
    if (tabId === 'tab-feeds') {
      loadFeedsList();
    } else if (tabId === 'tab-filter') {
      updateFilterPreview();
    } else if (tabId === 'tab-scraper') {
      loadScraperStats();
    } else if (tabId === 'tab-trending') {
      loadTrendingStatus();
    } else if (tabId === 'tab-clients') {
      loadDownloadClients();
    } else if (tabId === 'tab-maintenance') {
      loadJanitorStats();
    }
  }

  navItems.forEach(item => {
    item.addEventListener('click', () => {
      const tabId = item.getAttribute('data-tab');
      switchTab(tabId);
    });
  });

  // Handle initial deep-linking from hash
  const initialHash = window.location.hash.replace('#', '');
  if (initialHash) {
    const targetTabId = `tab-${initialHash}`;
    if (document.getElementById(targetTabId)) {
      switchTab(targetTabId, false);
    }
  }

  window.addEventListener('hashchange', () => {
    const hash = window.location.hash.replace('#', '');
    if (hash) {
      const targetTabId = `tab-${hash}`;
      if (document.getElementById(targetTabId)) {
        switchTab(targetTabId, false);
      }
    }
  });
}

// Global Form Listeners
function initFormListeners() {
  const form = document.getElementById('settings-form');
  const headerSaveBtn = document.getElementById('btn-header-save');

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    saveConfig();
  });

  if (headerSaveBtn) {
    headerSaveBtn.addEventListener('click', () => {
      saveConfig();
    });
  }
}

// Keyboard shortcuts (Ctrl+S / Cmd+S)
function initKeyboardShortcuts() {
  window.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault();
      saveConfig();
    }
  });
}

// API Key Masking & Copy Actions
function initKeyMasks() {
  document.querySelectorAll('.btn-toggle-mask').forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.preventDefault();
      const targetId = btn.getAttribute('data-target');
      const input = document.getElementById(targetId);
      if (input) {
        input.classList.toggle('unmasked');
        const isUnmasked = input.classList.contains('unmasked');
        btn.textContent = isUnmasked ? '🙈' : '👁️';
        btn.title = isUnmasked ? 'Hide value' : 'Show value';
      }
    });
  });

  document.querySelectorAll('.btn-copy-key').forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.preventDefault();
      const targetId = btn.getAttribute('data-target');
      const input = document.getElementById(targetId);
      if (input && input.value) {
        copyToClipboard(input.value);
        showToast('API key copied to clipboard!', 'success');
      }
    });
  });
}

function copyToClipboard(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text);
  } else {
    const temp = document.createElement('textarea');
    temp.value = text;
    document.body.appendChild(temp);
    temp.select();
    document.execCommand('copy');
    document.body.removeChild(temp);
  }
}

// Load System Config from API
async function loadConfig() {
  try {
    const res = await fetch('/api/v1/config');
    if (!res.ok) return;
    const cfg = await res.json();

    const setVal = (id, val) => {
      const el = document.getElementById(id);
      if (el) el.value = val ?? '';
    };

    setVal('cfg-prowlarr-url', cfg.prowlarr_url);
    setVal('cfg-prowlarr-key', cfg.prowlarr_api_key);
    setVal('cfg-torznab-key', cfg.torznab_api_key);
    setVal('cfg-dht-workers', cfg.dht_workers || 15);

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

    const autoPruneCheck = document.getElementById('cfg-auto-prune-dead');
    if (autoPruneCheck) autoPruneCheck.checked = !!cfg.auto_prune_dead;

    const autoPruneDays = document.getElementById('cfg-auto-prune-days');
    if (autoPruneDays && cfg.auto_prune_days) autoPruneDays.value = cfg.auto_prune_days;

    const filterEnabledCheck = document.getElementById('cfg-filter-enabled');
    if (filterEnabledCheck) filterEnabledCheck.checked = !!cfg.filter_enabled;

    const filterModeSelect = document.getElementById('cfg-filter-category-mode');
    if (filterModeSelect && cfg.filter_category_mode) filterModeSelect.value = cfg.filter_category_mode;

    const activeCats = new Set((cfg.filter_categories || []).map(c => c.toLowerCase()));
    document.querySelectorAll('.filter-cat-check').forEach(cb => {
      cb.checked = activeCats.has(cb.value.toLowerCase());
    });

    const filterKwInput = document.getElementById('cfg-filter-keywords');
    if (filterKwInput) filterKwInput.value = (cfg.filter_keywords || []).join(', ');

    const scraperEnabled = document.getElementById('cfg-scraper-enabled');
    if (scraperEnabled) scraperEnabled.checked = cfg.scraper_enabled !== false;

    const scraperInterval = document.getElementById('cfg-scraper-interval');
    if (scraperInterval) scraperInterval.value = cfg.scraper_interval_min || 30;

    const scraperBatch = document.getElementById('cfg-scraper-batch-size');
    if (scraperBatch) scraperBatch.value = cfg.scraper_batch_size || 50;

    const scraperAutoupdate = document.getElementById('cfg-scraper-autoupdate');
    if (scraperAutoupdate) scraperAutoupdate.checked = cfg.scraper_auto_update_trackers !== false;

    const scraperUrl = document.getElementById('cfg-scraper-trackers-url');
    if (scraperUrl) scraperUrl.value = cfg.scraper_trackers_url || '';

    const scraperTrackers = document.getElementById('cfg-scraper-trackers');
    if (scraperTrackers && cfg.scraper_trackers) scraperTrackers.value = cfg.scraper_trackers.join('\n');

    const trendingEnabled = document.getElementById('cfg-trending-enabled');
    if (trendingEnabled) trendingEnabled.checked = cfg.trending_enabled !== false;

    const trendingHours = document.getElementById('cfg-trending-interval-hours');
    if (trendingHours) trendingHours.value = cfg.trending_interval_hours || 4;

    const trendingMinSeeds = document.getElementById('cfg-trending-min-seeds');
    if (trendingMinSeeds) trendingMinSeeds.value = cfg.trending_min_seeds || 5;

    if (cfg.logging) {
      setVal('cfg-log-level', cfg.logging.level || 'INFO');
      setVal('cfg-log-file-path', cfg.logging.file_path || 'omnidex.log');
      setVal('cfg-log-max-size', cfg.logging.max_size_mb || 10);
      setVal('cfg-log-max-backups', cfg.logging.max_backups || 3);
      const logConsoleCheck = document.getElementById('cfg-log-to-console');
      if (logConsoleCheck) logConsoleCheck.checked = cfg.logging.log_to_console !== false;
    }

    renderTrendingProviders(cfg.trending_providers);
    loadTrendingStatus();

    updateFilterPreview();
  } catch (err) {
    console.error('Config fetch error:', err);
    showToast('Failed to load system configuration', 'error');
  }
}

// Save System Config to API
async function saveConfig() {
  const statusIndicator = document.getElementById('save-status-indicator');
  if (statusIndicator) statusIndicator.textContent = 'Saving...';

  const dlKb = parseFloat(document.getElementById('cfg-max-download-rate')?.value) || 0;
  const ulKb = parseFloat(document.getElementById('cfg-max-upload-rate')?.value) || 0;
  const monthlyGb = parseFloat(document.getElementById('cfg-monthly-data-limit')?.value) || 0;

  const filterCats = [];
  document.querySelectorAll('.filter-cat-check:checked').forEach(cb => filterCats.push(cb.value));
  const rawKeywords = document.getElementById('cfg-filter-keywords')?.value || '';
  const filterKeywords = rawKeywords.split(',').map(s => s.trim()).filter(s => s.length > 0);

  const trendingProviders = {};
  document.querySelectorAll('.trending-provider-check').forEach(cb => {
    trendingProviders[cb.dataset.provider] = cb.checked;
  });

  const payload = {
    trending_enabled: document.getElementById('cfg-trending-enabled')?.checked || false,
    trending_interval_hours: parseInt(document.getElementById('cfg-trending-interval-hours')?.value, 10) || 4,
    trending_min_seeds: parseInt(document.getElementById('cfg-trending-min-seeds')?.value, 10) || 5,
    trending_providers: trendingProviders,
    prowlarr_url: document.getElementById('cfg-prowlarr-url')?.value?.trim() || '',
    prowlarr_api_key: document.getElementById('cfg-prowlarr-key')?.value?.trim() || '',
    torznab_api_key: document.getElementById('cfg-torznab-key')?.value?.trim() || '',
    dht_workers: parseInt(document.getElementById('cfg-dht-workers')?.value, 10) || 15,
    max_download_rate: Math.max(0, Math.round(dlKb * 1024)),
    max_upload_rate: Math.max(0, Math.round(ulKb * 1024)),
    monthly_data_limit_bytes: Math.max(0, Math.round(monthlyGb * 1024 * 1024 * 1024)),
    auto_prune_dead: document.getElementById('cfg-auto-prune-dead')?.checked || false,
    auto_prune_days: parseInt(document.getElementById('cfg-auto-prune-days')?.value, 10) || 30,
    filter_enabled: document.getElementById('cfg-filter-enabled')?.checked || false,
    filter_category_mode: document.getElementById('cfg-filter-category-mode')?.value || 'allowlist',
    filter_categories: filterCats,
    filter_keywords: filterKeywords,
    scraper_enabled: document.getElementById('cfg-scraper-enabled')?.checked || false,
    scraper_interval_min: parseInt(document.getElementById('cfg-scraper-interval')?.value, 10) || 30,
    scraper_batch_size: parseInt(document.getElementById('cfg-scraper-batch-size')?.value, 10) || 50,
    scraper_auto_update_trackers: document.getElementById('cfg-scraper-autoupdate')?.checked || false,
    scraper_trackers_url: document.getElementById('cfg-scraper-trackers-url')?.value?.trim() || '',
    scraper_trackers: (document.getElementById('cfg-scraper-trackers')?.value || '')
      .split('\n')
      .map(t => t.trim())
      .filter(t => t.startsWith('udp://')),
    logging: {
      level: document.getElementById('cfg-log-level')?.value || 'INFO',
      file_path: document.getElementById('cfg-log-file-path')?.value?.trim() || 'omnidex.log',
      max_size_mb: parseInt(document.getElementById('cfg-log-max-size')?.value, 10) || 10,
      max_backups: parseInt(document.getElementById('cfg-log-max-backups')?.value, 10) || 3,
      log_to_console: document.getElementById('cfg-log-to-console')?.checked ?? true
    }
  };

  try {
    const resp = await fetch('/api/v1/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    if (resp.ok) {
      if (statusIndicator) statusIndicator.textContent = `Saved at ${new Date().toLocaleTimeString()}`;
      await saveDownloadClients();
      showToast('Settings saved successfully', 'success');
      loadConfig();
    } else {
      const errText = await resp.text();
      if (statusIndicator) statusIndicator.textContent = 'Save failed';
      showToast('Failed to save config: ' + errText, 'error');
    }
  } catch (err) {
    if (statusIndicator) statusIndicator.textContent = 'Save failed';
    showToast('Error saving settings: ' + err.message, 'error');
  }
}

// Feeds Management
function initFeeds() {
  document.getElementById('btn-add-feed')?.addEventListener('click', handleAddFeed);
  document.getElementById('btn-sync-all-feeds')?.addEventListener('click', handleSyncAllFeeds);

  document.getElementById('btn-cancel-delete-feed')?.addEventListener('click', () => {
    closeDeleteFeedModal();
  });

  document.getElementById('btn-confirm-delete-feed')?.addEventListener('click', async () => {
    if (!pendingDeleteFeedId) return;
    try {
      const resp = await fetch('/api/v1/feeds/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: pendingDeleteFeedId })
      });
      if (resp.ok) {
        showToast('Feed deleted successfully', 'success');
        loadFeedsList();
      } else {
        showToast('Failed to delete feed', 'error');
      }
    } catch (e) {
      showToast('Error deleting feed: ' + e.message, 'error');
    } finally {
      closeDeleteFeedModal();
    }
  });
}

function openDeleteFeedModal(id, name) {
  pendingDeleteFeedId = id;
  const nameEl = document.getElementById('delete-feed-name');
  if (nameEl) nameEl.textContent = name;
  const modal = document.getElementById('modal-confirm-delete-feed');
  if (modal) modal.classList.add('active');
}

function closeDeleteFeedModal() {
  pendingDeleteFeedId = null;
  const modal = document.getElementById('modal-confirm-delete-feed');
  if (modal) modal.classList.remove('active');
}

async function loadFeedsList() {
  const container = document.getElementById('feeds-list-container');
  if (!container) return;

  try {
    const res = await fetch('/api/v1/feeds');
    if (!res.ok) throw new Error('Failed to fetch feeds');
    const feeds = await res.json();

    const badgeFeeds = document.getElementById('badge-feeds-count');
    if (badgeFeeds) badgeFeeds.textContent = `${feeds.length} Active`;

    if (!feeds || feeds.length === 0) {
      container.innerHTML = `
        <div style="text-align: center; padding: 24px; color: var(--text-dim); background: rgba(0,0,0,0.2); border-radius: var(--radius-sm); border: 1px dashed var(--border-color);">
          No external feeds configured yet. Add your first RSS, EZTV, YTS, The Pirate Bay, or Torrents.csv feed below.
        </div>`;
      return;
    }

    container.innerHTML = feeds.map(feed => {
      const lastSync = feed.last_sync ? new Date(feed.last_sync).toLocaleTimeString() : 'Never';
      const typeLabel = (feed.type || 'rss').toUpperCase();
      const statusColor = feed.last_error ? '#ef4444' : '#10b981';
      const statusText = feed.last_error ? `Error: ${feed.last_error}` : `OK · Last: ${lastSync}`;
      const interval = feed.interval_minutes || feed.interval_min || 30;
      const count = feed.last_count !== undefined ? feed.last_count : (feed.item_count || 0);

      return `
        <div class="feed-item-card" data-feed-id="${feed.id}">
          <div style="flex: 1; min-width: 0; overflow: hidden;">
            <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 4px; flex-wrap: wrap;">
              <strong style="color: var(--text-main); font-size: 0.95rem;">${escapeHtml(feed.name)}</strong>
              <span class="badge" style="background: rgba(6, 182, 212, 0.15); color: var(--accent-cyan); font-size: 0.72rem;">${typeLabel}</span>
              <span style="font-size: 0.78rem; color: var(--text-dim);">Every ${interval}m</span>
            </div>
            <div style="font-size: 0.8rem; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: monospace;" title="${escapeHtml(feed.url)}">
              ${escapeHtml(feed.url)}
            </div>
            <div style="font-size: 0.75rem; color: ${statusColor}; margin-top: 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(statusText)}">
              ${escapeHtml(statusText)} · Items: ${count}
            </div>
          </div>
          <div style="display: flex; align-items: center; gap: 8px; flex-shrink: 0;">
            <button type="button" class="btn btn-secondary btn-sync-single-feed" data-id="${feed.id}" style="padding: 6px 10px; font-size: 0.78rem;" title="Sync feed now">🔄 Sync</button>
            <button type="button" class="btn btn-secondary btn-icon-delete" data-id="${feed.id}" data-name="${escapeHtml(feed.name)}" style="padding: 6px 10px; font-size: 0.78rem;" title="Delete feed">🗑️</button>
          </div>
        </div>
      `;
    }).join('');

    container.querySelectorAll('.btn-sync-single-feed').forEach(btn => {
      btn.addEventListener('click', async (e) => {
        const id = btn.getAttribute('data-id');
        btn.disabled = true;
        btn.textContent = '⌛...';
        try {
          const resp = await fetch('/api/v1/feeds/sync', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ feed_id: id })
          });
          const res = await resp.json();
          if (resp.ok) {
            showToast(`Sync complete: ${res.indexed || 0} indexed, ${res.duplicates || 0} duplicates`, 'success');
            loadFeedsList();
          } else {
            showToast(`Sync failed: ${res.error}`, 'error');
          }
        } catch (err) {
          showToast(`Sync error: ${err.message}`, 'error');
        } finally {
          btn.disabled = false;
          btn.textContent = '🔄 Sync';
        }
      });
    });

    container.querySelectorAll('.btn-icon-delete').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = btn.getAttribute('data-id');
        const name = btn.getAttribute('data-name');
        openDeleteFeedModal(id, name);
      });
    });
  } catch (e) {
    console.error('Error loading feeds:', e);
  }
}

async function handleAddFeed() {
  const nameInput = document.getElementById('new-feed-name');
  const typeSelect = document.getElementById('new-feed-type');
  const urlInput = document.getElementById('new-feed-url');
  const apiKeyInput = document.getElementById('new-feed-apikey');
  const intervalInput = document.getElementById('new-feed-interval');

  const name = nameInput.value.trim();
  const type = typeSelect.value;
  const url = urlInput.value.trim();
  const apiKey = apiKeyInput.value.trim();
  const interval = parseInt(intervalInput.value, 10) || 30;

  if (!name || !url) {
    showToast('Feed Name and URL are required', 'warning');
    return;
  }

  try {
    const resp = await fetch('/api/v1/feeds', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: name,
        type: type,
        url: url,
        api_key: apiKey,
        interval_min: interval
      })
    });
    if (resp.ok) {
      showToast('Feed added successfully', 'success');
      nameInput.value = '';
      urlInput.value = '';
      apiKeyInput.value = '';
      intervalInput.value = '30';
      loadFeedsList();
    } else {
      const err = await resp.text();
      showToast('Failed to add feed: ' + err, 'error');
    }
  } catch (e) {
    showToast('Error adding feed: ' + e.message, 'error');
  }
}

async function handleSyncAllFeeds() {
  const btn = document.getElementById('btn-sync-all-feeds');
  if (btn) {
    btn.disabled = true;
    btn.textContent = '🔄 Syncing All Feeds...';
  }
  try {
    const resp = await fetch('/api/v1/feeds/sync', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({})
    });
    const res = await resp.json();
    if (resp.ok) {
      showToast(`All feeds synced: ${res.indexed || 0} new indexed, ${res.duplicates || 0} duplicates`, 'success');
      loadFeedsList();
    } else {
      showToast('Sync error: ' + (res.error || 'Unknown error'), 'error');
    }
  } catch (err) {
    showToast('Sync error: ' + err.message, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = '🔄 Sync All Feeds Now';
    }
  }
}

// Scraper Dashboard
async function loadScraperStats() {
  try {
    const res = await fetch('/api/v1/scraper/status');
    if (!res.ok) return;
    const stats = await res.json();

    const setEl = (id, text) => {
      const el = document.getElementById(id);
      if (el) el.textContent = text;
    };

    setEl('scraper-stat-total', stats.total_scraped || 0);
    setEl('scraper-stat-success', stats.successful ?? stats.successful_scrapes ?? 0);
    setEl('scraper-stat-trackers', stats.active_trackers || 0);

    const badge = document.getElementById('scraper-live-status-badge');
    if (badge) {
      const isRunning = stats.running ?? stats.enabled ?? false;
      if (isRunning) {
        badge.textContent = 'Active';
        badge.style.background = 'rgba(34, 197, 94, 0.2)';
        badge.style.color = '#4ade80';
      } else {
        badge.textContent = 'Disabled';
        badge.style.background = 'rgba(239, 68, 68, 0.2)';
        badge.style.color = '#f87171';
      }
    }

    if (stats.last_scrape_time) {
      const d = new Date(stats.last_scrape_time);
      setEl('scraper-stat-last', d.toLocaleTimeString());
    } else {
      setEl('scraper-stat-last', 'Never');
    }
  } catch (e) {
    console.error('Error fetching scraper stats:', e);
  }
}

// Direct Import & CSV Bulk Ingest
function initDirectImport() {
  const btnUploadCsv = document.getElementById('btn-upload-csv');
  const fileInput = document.getElementById('import-csv-file');
  const btnExecute = document.getElementById('btn-execute-import');

  if (btnUploadCsv && fileInput) {
    btnUploadCsv.addEventListener('click', () => fileInput.click());
    fileInput.addEventListener('change', (e) => {
      const file = e.target.files?.[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = (event) => {
        const text = event.target?.result;
        const inputArea = document.getElementById('import-input');
        if (inputArea && typeof text === 'string') {
          inputArea.value = text;
          showToast(`Loaded "${file.name}" (${(file.size / 1024).toFixed(1)} KB)`, 'success');
        }
      };
      reader.readAsText(file);
    });
  }

  if (btnExecute) {
    btnExecute.addEventListener('click', handleExecuteImport);
  }
}

async function handleExecuteImport() {
  const inputEl = document.getElementById('import-input');
  const nameEl = document.getElementById('import-name');
  const statusMsg = document.getElementById('import-status-msg');
  const btn = document.getElementById('btn-execute-import');

  const rawInput = inputEl?.value?.trim() || '';
  const customName = nameEl?.value?.trim() || '';

  if (!rawInput) {
    showToast('Please enter a Magnet link, InfoHash, or paste CSV lines', 'warning');
    return;
  }

  if (btn) {
    btn.disabled = true;
    btn.textContent = '⏳ Processing import...';
  }

  try {
    const resp = await fetch('/api/v1/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        input: rawInput,
        name: customName
      })
    });
    const result = await resp.json();

    if (resp.ok && result.success) {
      if (statusMsg) {
        statusMsg.style.display = 'block';
        statusMsg.style.background = 'rgba(16, 185, 129, 0.15)';
        statusMsg.style.border = '1px solid rgba(16, 185, 129, 0.3)';
        statusMsg.style.color = '#34d399';
        statusMsg.textContent = result.message || 'Import completed successfully.';
      }
      showToast(result.message || 'Import successful!', 'success');
      if (inputEl) inputEl.value = '';
      if (nameEl) nameEl.value = '';
    } else {
      if (statusMsg) {
        statusMsg.style.display = 'block';
        statusMsg.style.background = 'rgba(239, 68, 68, 0.15)';
        statusMsg.style.border = '1px solid rgba(239, 68, 68, 0.3)';
        statusMsg.style.color = '#f87171';
        statusMsg.textContent = result.error || 'Import failed. Check format.';
      }
      showToast(result.error || 'Import failed', 'error');
    }
  } catch (err) {
    showToast('Network error during import: ' + err.message, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = '🧲 Import & Index Torrent(s)';
    }
  }
}

// Rules & Retention Preview & Purge
function initRulesFilter() {
  ['cfg-filter-enabled', 'cfg-filter-category-mode'].forEach(id => {
    document.getElementById(id)?.addEventListener('change', updateFilterPreview);
  });
  document.querySelectorAll('.filter-cat-check').forEach(cb => {
    cb.addEventListener('change', updateFilterPreview);
  });
  document.getElementById('cfg-filter-keywords')?.addEventListener('input', () => {
    clearTimeout(filterKwTimer);
    filterKwTimer = setTimeout(updateFilterPreview, 350);
  });
  document.getElementById('btn-preview-filter-purge')?.addEventListener('click', updateFilterPreview);
  document.getElementById('btn-execute-filter-purge')?.addEventListener('click', handleExecuteFilterPurge);
}

async function updateFilterPreview() {
  const badge = document.getElementById('filter-unmatched-badge');
  if (!badge) return;

  const mode = document.getElementById('cfg-filter-category-mode')?.value || 'allowlist';
  const selectedCats = [];
  document.querySelectorAll('.filter-cat-check:checked').forEach(cb => selectedCats.push(cb.value));
  const rawKeywords = document.getElementById('cfg-filter-keywords')?.value || '';

  const params = new URLSearchParams({
    mode: mode,
    categories: selectedCats.join(','),
    keywords: rawKeywords
  });

  try {
    const res = await fetch(`/api/v1/torrents/filter-preview?${params.toString()}`);
    if (!res.ok) return;
    const data = await res.json();
    const count = data.unmatched_count || 0;
    badge.textContent = `${count.toLocaleString()} unmatched`;
    badge.style.background = count > 0 ? 'rgba(239, 68, 68, 0.2)' : 'rgba(34, 197, 94, 0.2)';
    badge.style.color = count > 0 ? '#f87171' : '#4ade80';
  } catch (e) {
    console.error('Error fetching filter preview:', e);
  }
}

async function handleExecuteFilterPurge() {
  const mode = document.getElementById('cfg-filter-category-mode')?.value || 'allowlist';
  const selectedCats = [];
  document.querySelectorAll('.filter-cat-check:checked').forEach(cb => selectedCats.push(cb.value));
  const rawKeywords = document.getElementById('cfg-filter-keywords')?.value || '';
  const filterKeywords = rawKeywords.split(',').map(s => s.trim()).filter(s => s.length > 0);

  if (!confirm('Are you sure you want to purge all torrents in the database that do not match these criteria? This cannot be undone.')) {
    return;
  }

  const btn = document.getElementById('btn-execute-filter-purge');
  const msg = document.getElementById('filter-purge-msg');
  if (btn) {
    btn.disabled = true;
    btn.textContent = '🗑️ Purging...';
  }

  try {
    const resp = await fetch('/api/v1/torrents/filter-purge', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        mode: mode,
        categories: selectedCats,
        keywords: filterKeywords
      })
    });
    const result = await resp.json();
    if (resp.ok && result.success) {
      if (msg) {
        msg.style.display = 'block';
        msg.style.background = 'rgba(16, 185, 129, 0.15)';
        msg.style.border = '1px solid rgba(16, 185, 129, 0.3)';
        msg.style.color = '#34d399';
        msg.textContent = `Successfully purged ${result.purged_count || 0} torrents.`;
      }
      showToast(`Purged ${result.purged_count || 0} unmatched torrents`, 'success');
      updateFilterPreview();
    } else {
      showToast('Purge failed: ' + (result.error || 'Unknown error'), 'error');
    }
  } catch (e) {
    showToast('Error during purge: ' + e.message, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = '🗑️ Purge Unmatched Now';
    }
  }
}

// Memory Janitor & System Stats
function initJanitor() {
  document.getElementById('btn-janitor-shrink')?.addEventListener('click', async () => {
    const btn = document.getElementById('btn-janitor-shrink');
    if (btn) {
      btn.disabled = true;
      btn.textContent = '🧹 Cleaning...';
    }
    try {
      const resp = await fetch('/api/v1/memory/shrink', { method: 'POST' });
      const data = await resp.json();
      if (resp.ok) {
        showToast(data.message || 'Memory cleaned and WAL checkpointed', 'success');
        loadJanitorStats();
      } else {
        showToast('Clean failed', 'error');
      }
    } catch (e) {
      showToast('Error cleaning memory: ' + e.message, 'error');
    } finally {
      if (btn) {
        btn.disabled = false;
        btn.textContent = '🧹 Free Memory Now';
      }
    }
  });
}

async function loadJanitorStats() {
  try {
    const res = await fetch('/api/v1/stats');
    if (!res.ok) return;
    const stats = await res.json();

    const setVal = (id, text) => {
      const el = document.getElementById(id);
      if (el) el.textContent = text;
    };

    if (stats.mem_rss_bytes) setVal('janitor-rss', formatBytes(stats.mem_rss_bytes));
    if (stats.mem_heap_alloc_bytes) setVal('janitor-heap', formatBytes(stats.mem_heap_alloc_bytes));
    if (stats.mem_sys_bytes) setVal('janitor-sys', formatBytes(stats.mem_sys_bytes));
    if (stats.num_goroutines) setVal('janitor-goroutines', `${stats.num_goroutines} routines`);
  } catch (e) {
    console.error('Error fetching janitor stats:', e);
  }
}

// Utility: Toasts
function showToast(message, type = 'info', duration = 3500) {
  const container = document.getElementById('toast-container');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;

  const icon = type === 'success' ? '✓' : type === 'error' ? '✕' : type === 'warning' ? '⚠' : 'ℹ';
  toast.innerHTML = `
    <span class="toast-icon">${icon}</span>
    <span class="toast-message">${escapeHtml(message)}</span>
    <button class="toast-close" title="Dismiss">&times;</button>
  `;

  toast.querySelector('.toast-close').addEventListener('click', () => {
    toast.classList.remove('toast-show');
    setTimeout(() => toast.remove(), 250);
  });

  container.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add('toast-show'));

  setTimeout(() => {
    if (toast.parentElement) {
      toast.classList.remove('toast-show');
      setTimeout(() => toast.remove(), 250);
    }
  }, duration);
}

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}



// Trending Harvester Client Logic
const HARVESTER_PROVIDERS = [
  { id: 'thepiratebay', name: 'The Pirate Bay (Apibay)', desc: 'Top 100 recent/all categories', default: true },
  { id: 'yts', name: 'YTS (Movies)', desc: 'Top-seeded 720p/1080p/4K releases', default: true },
  { id: 'eztv', name: 'EZTV (TV Shows)', desc: 'Latest airing TV episodes', default: true },
  { id: 'nyaa', name: 'Nyaa (Anime & Media)', desc: 'Top-seeded anime releases', default: true },
  { id: 'torrentdownloads', name: 'TorrentDownloads', desc: 'Daily popular releases & general media', default: true },
  { id: 'oxtorrent', name: 'OxTorrent (French/EU)', desc: 'European movies and series', default: false },
  { id: 'audiobookbay', name: 'AudioBookBay', desc: 'Popular audiobooks and audio series', default: false },
  { id: 'sukebei', name: 'Sukebei', desc: 'Adult anime and media', default: false }
];

function renderTrendingProviders(providersConfig) {
  const container = document.getElementById('trending-providers-grid');
  if (!container) return;

  const conf = providersConfig || {};
  container.innerHTML = HARVESTER_PROVIDERS.map(p => {
    const isChecked = conf[p.id] !== undefined ? !!conf[p.id] : p.default;
    return `
      <label class="provider-card" style="display: flex; align-items: flex-start; gap: 10px; padding: 12px; border: 1px solid var(--border-color); border-radius: var(--radius-sm); background: rgba(255, 255, 255, 0.02); cursor: pointer;">
        <input type="checkbox" class="trending-provider-check" data-provider="${p.id}" ${isChecked ? 'checked' : ''} style="margin-top: 3px;">
        <div style="flex: 1; min-width: 0;">
          <div style="font-weight: 600; font-size: 0.9rem; color: var(--text-main);">${escapeHtml(p.name)}</div>
          <div style="font-size: 0.75rem; color: var(--text-dim); margin-top: 2px;">${escapeHtml(p.desc)}</div>
        </div>
      </label>
    `;
  }).join('');
}

async function loadTrendingStatus() {
  try {
    const res = await fetch('/api/v1/trending/status');
    if (!res.ok) return;
    const stats = await res.json();

    const statusEl = document.getElementById('harvest-stat-status');
    if (statusEl) {
      if (stats.is_harvesting) {
        statusEl.textContent = 'Harvesting...';
        statusEl.style.color = '#38bdf8';
      } else if (stats.running) {
        statusEl.textContent = 'Active';
        statusEl.style.color = '#4ade80';
      } else {
        statusEl.textContent = 'Disabled';
        statusEl.style.color = 'var(--text-dim)';
      }
    }

    const lastCountEl = document.getElementById('harvest-stat-last-count');
    if (lastCountEl) {
      lastCountEl.textContent = `${stats.last_count || 0} torrents`;
    }

    const totalEl = document.getElementById('harvest-stat-total');
    if (totalEl) {
      totalEl.textContent = stats.total_harvested || 0;
    }

    const lastRunEl = document.getElementById('harvest-stat-last-run');
    if (lastRunEl) {
      if (stats.last_run && !stats.last_run.startsWith('0001')) {
        const d = new Date(stats.last_run);
        lastRunEl.textContent = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        lastRunEl.title = d.toLocaleString();
      } else {
        lastRunEl.textContent = 'Never';
      }
    }
  } catch (err) {
    console.warn('Failed to load trending status:', err);
  }
}

function initTrendingHarvester() {
  const btnHarvest = document.getElementById('btn-harvest-now');
  if (!btnHarvest) return;

  btnHarvest.addEventListener('click', async () => {
    const icon = document.getElementById('harvest-btn-icon');
    const text = document.getElementById('harvest-btn-text');
    const origText = text ? text.textContent : 'Harvest Trending Now';

    if (icon) icon.textContent = '⏳';
    if (text) text.textContent = 'Harvesting...';
    btnHarvest.disabled = true;

    try {
      const res = await fetch('/api/v1/trending/harvest', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      });
      const data = await res.json();
      if (res.ok) {
        showToast(data.message || `Harvest complete: ${data.count} high-seed torrents ingested!`, 'success');
      } else {
        showToast(data.error || 'Harvest failed', 'error');
      }
      loadTrendingStatus();
    } catch (err) {
      showToast('Error triggering harvest: ' + err.message, 'error');
    } finally {
      if (icon) icon.textContent = '🚀';
      if (text) text.textContent = origText;
      btnHarvest.disabled = false;
    }
  });
}

// --- Download Clients Management ---

let currentClients = [];

function initDownloadClients() {
  const btnAdd = document.getElementById('btn-add-client');
  if (btnAdd) {
    btnAdd.addEventListener('click', () => {
      const newClient = {
        id: 'client_' + Date.now().toString(36),
        name: 'My qBittorrent',
        type: 'qbittorrent',
        host: 'http://localhost:8080',
        username: 'admin',
        password: '',
        save_path: '',
        category: '',
        auto_start: true,
        is_default: currentClients.length === 0,
        enabled: true
      };
      currentClients.push(newClient);
      renderDownloadClientsList(currentClients);
    });
  }
}

async function loadDownloadClients() {
  try {
    const res = await fetch('/api/v1/clients');
    if (!res.ok) return;
    const clients = await res.json();
    currentClients = clients || [];
    renderDownloadClientsList(currentClients);
  } catch (err) {
    console.error('Error loading download clients:', err);
  }
}

function renderDownloadClientsList(clients) {
  const container = document.getElementById('clients-list-container');
  if (!container) return;
  container.innerHTML = '';

  if (!clients || clients.length === 0) {
    container.innerHTML = `
      <div style="background: rgba(0,0,0,0.25); border: 1px dashed var(--border-color); border-radius: var(--radius-sm); padding: 28px; text-align: center;">
        <span style="font-size: 2rem; display: block; margin-bottom: 8px;">🚀</span>
        <h4 style="color: #fff; margin: 0 0 6px 0;">No Download Clients Configured</h4>
        <p style="color: var(--text-muted); font-size: 0.85rem; margin: 0 0 16px 0;">
          Connect your local or remote qBittorrent, Transmission, or Deluge instance to send torrents with a single click.
        </p>
        <button type="button" class="btn btn-secondary" onclick="document.getElementById('btn-add-client').click()">
          Add First Client
        </button>
      </div>
    `;
    return;
  }

  clients.forEach((c, idx) => {
    const card = document.createElement('div');
    card.style.background = 'rgba(0,0,0,0.3)';
    card.style.border = '1px solid var(--border-color)';
    card.style.borderRadius = 'var(--radius-sm)';
    card.style.padding = '18px';
    card.style.display = 'flex';
    card.style.flexDirection = 'column';
    card.style.gap = '14px';

    card.innerHTML = `
      <div style="display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid rgba(255,255,255,0.06); padding-bottom: 12px;">
        <div style="display: flex; align-items: center; gap: 10px;">
          <input type="checkbox" class="client-enable-toggle" data-idx="${idx}" ${c.enabled ? 'checked' : ''} style="accent-color: var(--accent-cyan); width: 17px; height: 17px; cursor: pointer;" title="Enable/Disable Client">
          <input type="text" class="form-input client-name-input" data-idx="${idx}" value="${escapeHtml(c.name)}" style="font-weight: 600; width: 220px;" placeholder="Client Name">
          <span class="badge" style="background: rgba(56, 189, 248, 0.15); color: #38bdf8; border: 1px solid rgba(56, 189, 248, 0.3);">${c.type.toUpperCase()}</span>
          ${c.is_default ? '<span class="badge badge-dht">DEFAULT</span>' : ''}
        </div>
        <div style="display: flex; gap: 8px; align-items: center;">
          <button type="button" class="btn btn-secondary btn-test-client" data-idx="${idx}" style="font-size: 0.8rem; padding: 5px 12px;">
            🔌 Test Connection
          </button>
          <button type="button" class="btn btn-secondary btn-default-client" data-idx="${idx}" style="font-size: 0.8rem; padding: 5px 10px;" title="Set as default client">
            ${c.is_default ? '★ Default' : '☆ Make Default'}
          </button>
          <button type="button" class="btn btn-delete-client" data-idx="${idx}" style="background: rgba(239, 68, 68, 0.2); border: 1px solid rgba(239, 68, 68, 0.4); color: #f87171; font-size: 0.8rem; padding: 5px 10px;" title="Remove client">
            🗑️
          </button>
        </div>
      </div>

      <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px;">
        <div class="form-group" style="margin: 0;">
          <label style="font-size: 0.78rem;">Client Type</label>
          <select class="custom-select client-type-select" data-idx="${idx}" style="width: 100%;">
            <option value="qbittorrent" ${c.type === 'qbittorrent' ? 'selected' : ''}>qBittorrent (Web API v2)</option>
            <option value="transmission" ${c.type === 'transmission' ? 'selected' : ''}>Transmission (RPC)</option>
            <option value="deluge" ${c.type === 'deluge' ? 'selected' : ''}>Deluge (Web JSON-RPC)</option>
          </select>
        </div>

        <div class="form-group" style="margin: 0;">
          <label style="font-size: 0.78rem;">Host URL (with port)</label>
          <input type="text" class="form-input client-host-input" data-idx="${idx}" value="${escapeHtml(c.host)}" placeholder="http://localhost:8080">
        </div>

        <div class="form-group" style="margin: 0;">
          <label style="font-size: 0.78rem;">Username</label>
          <input type="text" class="form-input client-user-input" data-idx="${idx}" value="${escapeHtml(c.username || '')}" placeholder="admin">
        </div>

        <div class="form-group" style="margin: 0;">
          <label style="font-size: 0.78rem;">Password / Token</label>
          <input type="password" class="form-input client-pass-input" data-idx="${idx}" value="${escapeHtml(c.password || '')}" placeholder="••••••••">
        </div>

        <div class="form-group" style="margin: 0;">
          <label style="font-size: 0.78rem;">Default Save Path (Optional)</label>
          <input type="text" class="form-input client-path-input" data-idx="${idx}" value="${escapeHtml(c.save_path || '')}" placeholder="/downloads">
        </div>

        <div class="form-group" style="margin: 0;">
          <label style="font-size: 0.78rem;">Default Category (Optional)</label>
          <input type="text" class="form-input client-cat-input" data-idx="${idx}" value="${escapeHtml(c.category || '')}" placeholder="Movies">
        </div>
      </div>

      <div style="display: flex; align-items: center; gap: 10px; font-size: 0.82rem; color: var(--text-dim); margin-top: 4px;">
        <label style="display: flex; align-items: center; gap: 6px; cursor: pointer;">
          <input type="checkbox" class="client-autostart-check" data-idx="${idx}" ${c.auto_start !== false ? 'checked' : ''} style="accent-color: var(--accent-cyan); cursor: pointer;">
          <span>Start torrent download immediately (unchecked = add paused)</span>
        </label>
      </div>
    `;

    container.appendChild(card);
  });

  // Attach event handlers
  container.querySelectorAll('.client-enable-toggle').forEach(el => {
    el.addEventListener('change', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].enabled = el.checked;
    });
  });

  container.querySelectorAll('.client-name-input').forEach(el => {
    el.addEventListener('input', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].name = el.value.trim();
    });
  });

  container.querySelectorAll('.client-type-select').forEach(el => {
    el.addEventListener('change', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].type = el.value;
      renderDownloadClientsList(currentClients);
    });
  });

  container.querySelectorAll('.client-host-input').forEach(el => {
    el.addEventListener('input', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].host = el.value.trim();
    });
  });

  container.querySelectorAll('.client-user-input').forEach(el => {
    el.addEventListener('input', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].username = el.value.trim();
    });
  });

  container.querySelectorAll('.client-pass-input').forEach(el => {
    el.addEventListener('input', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].password = el.value;
    });
  });

  container.querySelectorAll('.client-path-input').forEach(el => {
    el.addEventListener('input', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].save_path = el.value.trim();
    });
  });

  container.querySelectorAll('.client-cat-input').forEach(el => {
    el.addEventListener('input', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].category = el.value.trim();
    });
  });

  container.querySelectorAll('.client-autostart-check').forEach(el => {
    el.addEventListener('change', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients[idx].auto_start = el.checked;
    });
  });

  container.querySelectorAll('.btn-default-client').forEach(el => {
    el.addEventListener('click', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients.forEach((cl, i) => cl.is_default = (i === idx));
      renderDownloadClientsList(currentClients);
    });
  });

  container.querySelectorAll('.btn-delete-client').forEach(el => {
    el.addEventListener('click', (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      currentClients.splice(idx, 1);
      if (currentClients.length > 0 && !currentClients.some(c => c.is_default)) {
        currentClients[0].is_default = true;
      }
      renderDownloadClientsList(currentClients);
    });
  });

  container.querySelectorAll('.btn-test-client').forEach(el => {
    el.addEventListener('click', async (e) => {
      const idx = parseInt(el.dataset.idx, 10);
      const c = currentClients[idx];
      el.disabled = true;
      const origText = el.textContent;
      el.textContent = '⏳ Testing...';

      try {
        const res = await fetch('/api/v1/clients/test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(c)
        });
        const data = await res.json();
        if (res.ok && data.success) {
          showToast(data.message || `Connected to ${c.name} successfully!`, 'success');
        } else {
          showToast(`Connection test failed: ${data.error || 'Server error'}`, 'error');
        }
      } catch (err) {
        showToast(`Test failed: ${err.message}`, 'error');
      } finally {
        el.disabled = false;
        el.textContent = origText;
      }
    });
  });
}

async function saveDownloadClients() {
  try {
    const res = await fetch('/api/v1/clients', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(currentClients)
    });
    if (!res.ok) {
      const err = await res.text();
      showToast('Failed to save download clients: ' + err, 'error');
    }
  } catch (err) {
    console.error('Error saving download clients:', err);
  }
}
