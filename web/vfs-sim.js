/**
 * OpenScion 0-Byte Ghost VFS & Monorepo Benchmark Calculator
 */

(function () {
  // 1. Ghost VFS Hydration Explorer
  const vfsListContainer = document.getElementById('vfs-file-list');
  const vfsTotalHydrated = document.getElementById('vfs-total-hydrated');
  const vfsTotalFiles = document.getElementById('vfs-total-files');
  const btnHydrateAll = document.getElementById('btn-vfs-hydrate-all');
  const btnPurgeVfs = document.getElementById('btn-vfs-purge');

  const VIRTUAL_FILES = [
    { name: 'services/auth/token_lease.go', size: '14.2 KB', sizeBytes: 14500, hydrated: false },
    { name: 'services/payments/stripe_reconcile.go', size: '38.6 KB', sizeBytes: 39500, hydrated: false },
    { name: 'pkg/cas/fastcdc_chunker.go', size: '28.1 KB', sizeBytes: 28700, hydrated: false },
    { name: 'frontend/apps/console/src/index.tsx', size: '52.4 KB', sizeBytes: 53600, hydrated: false },
    { name: 'config/production_cluster.json', size: '4.8 KB', sizeBytes: 4900, hydrated: false },
    { name: 'ml/embeddings/cache_table.arrow', size: '182.0 KB', sizeBytes: 186000, hydrated: false },
    { name: 'proto/vcs/changeset_service.proto', size: '16.5 KB', sizeBytes: 16900, hydrated: false }
  ];

  let hydratedBytes = 0;

  function renderVfsList() {
    if (!vfsListContainer) return;
    vfsListContainer.innerHTML = '';

    VIRTUAL_FILES.forEach((file, idx) => {
      const entry = document.createElement('div');
      entry.className = 'vfs-file-entry';
      entry.innerHTML = `
        <div class="vfs-file-name">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="${file.hydrated ? '#10b981' : '#64748b'}" stroke-width="2">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
            <polyline points="14 2 14 8 20 8"></polyline>
          </svg>
          <span style="color: ${file.hydrated ? '#fff' : '#94a3b8'}">${file.name}</span>
        </div>
        <div>
          ${file.hydrated ? 
            `<span class="vfs-file-stat hydrated">✔ Hydrated (${file.size})</span>` : 
            `<span class="vfs-file-stat ghost">0 B (Click to read)</span>`
          }
        </div>
      `;

      entry.addEventListener('click', () => {
        if (!file.hydrated) {
          hydrateFile(idx);
        }
      });

      vfsListContainer.appendChild(entry);
    });

    updateVfsStats();
  }

  function hydrateFile(idx) {
    const file = VIRTUAL_FILES[idx];
    if (file.hydrated) return;

    file.hydrated = true;
    hydratedBytes += file.sizeBytes;
    renderVfsList();

    if (window.showToast) {
      window.showToast(`Hydrated ${file.name} from CAS in 2.8ms`);
    }
  }

  function updateVfsStats() {
    const hydratedCount = VIRTUAL_FILES.filter(f => f.hydrated).length;
    if (vfsTotalFiles) vfsTotalFiles.textContent = `${hydratedCount} / ${VIRTUAL_FILES.length} files`;
    if (vfsTotalHydrated) {
      const kb = (hydratedBytes / 1024).toFixed(1);
      vfsTotalHydrated.textContent = hydratedBytes === 0 ? '0 Bytes (Ghost)' : `${kb} KB`;
    }
  }

  if (btnHydrateAll) {
    btnHydrateAll.addEventListener('click', () => {
      VIRTUAL_FILES.forEach(f => {
        if (!f.hydrated) {
          f.hydrated = true;
          hydratedBytes += f.sizeBytes;
        }
      });
      renderVfsList();
      if (window.showToast) window.showToast('Hydrated all files via loopback WebDAV');
    });
  }

  if (btnPurgeVfs) {
    btnPurgeVfs.addEventListener('click', () => {
      VIRTUAL_FILES.forEach(f => f.hydrated = false);
      hydratedBytes = 0;
      renderVfsList();
      if (window.showToast) window.showToast('Purged local cache — returned to 0-byte ghost state');
    });
  }

  renderVfsList();

  // 2. Monorepo Benchmark Calculator Slider
  const slider = document.getElementById('bench-scale-slider');
  const scaleDisplay = document.getElementById('bench-scale-display');
  const gitTimeEl = document.getElementById('bench-git-time');
  const scionTimeEl = document.getElementById('bench-scion-time');
  const gitDiskEl = document.getElementById('bench-git-disk');
  const scionDiskEl = document.getElementById('bench-scion-disk');
  const gitEgressEl = document.getElementById('bench-git-egress');
  const scionEgressEl = document.getElementById('bench-scion-egress');

  const BENCH_STEPS = [
    { count: '25,000 files', gitTime: '12.4s', scionTime: '3.2ms', gitDisk: '2.8 GB', scionDisk: '0 Bytes', gitEgress: '45 TB / mo', scionEgress: '$0 (99.2% cut)' },
    { count: '100,000 files', gitTime: '48.2s', scionTime: '5.1ms', gitDisk: '12.4 GB', scionDisk: '0 Bytes', gitEgress: '180 TB / mo', scionEgress: '$0 (99.4% cut)' },
    { count: '500,000 files', gitTime: '3m 42s', scionTime: '8.6ms', gitDisk: '46.0 GB', scionDisk: '0 Bytes', gitEgress: '620 TB / mo', scionEgress: '$0 (99.6% cut)' },
    { count: '1,500,000 files', gitTime: '9m 15s', scionTime: '12.4ms', gitDisk: '135.0 GB', scionDisk: '0 Bytes', gitEgress: '1.8 PB / mo', scionEgress: '$0 (99.8% cut)' },
    { count: '3,000,000 files (Enterprise)', gitTime: '22m 30s', scionTime: '16.8ms', gitDisk: '280.0 GB', scionDisk: '0 Bytes', gitEgress: '3.6 PB / mo', scionEgress: '$0 (99.9% cut)' }
  ];

  function updateBenchmark(index) {
    const data = BENCH_STEPS[index] || BENCH_STEPS[1];
    if (scaleDisplay) scaleDisplay.textContent = data.count;
    if (gitTimeEl) gitTimeEl.textContent = data.gitTime;
    if (scionTimeEl) scionTimeEl.textContent = data.scionTime;
    if (gitDiskEl) gitDiskEl.textContent = data.gitDisk;
    if (scionDiskEl) scionDiskEl.textContent = data.scionDisk;
    if (gitEgressEl) gitEgressEl.textContent = data.gitEgress;
    if (scionEgressEl) scionEgressEl.textContent = data.scionEgress;
  }

  if (slider) {
    slider.addEventListener('input', (e) => {
      updateBenchmark(parseInt(e.target.value, 10));
    });
    updateBenchmark(1);
  }
})();
