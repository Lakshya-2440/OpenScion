/**
 * OpenScion Landing Page Main Application Script
 * Interactive controllers, Tabs, Command Palette, Install Switcher
 */

(function () {
  // 1. Toast Notification Helper
  const toast = document.getElementById('toast-notice');
  const toastMsg = document.getElementById('toast-msg');

  window.showToast = function (msg) {
    if (!toast || !toastMsg) return;
    toastMsg.textContent = msg;
    toast.classList.add('show');
    setTimeout(() => {
      toast.classList.remove('show');
    }, 3200);
  };

  // 2. Install Command Tabs & Copy Handler
  const installCmdText = document.getElementById('install-cmd-display');
  const btnCopyCmd = document.getElementById('btn-copy-install');
  const installTabs = document.querySelectorAll('.install-tab-btn');

  const INSTALL_COMMANDS = {
    darwin: 'curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash',
    linux: 'curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash',
    windows: 'irm https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.ps1 | iex',
    source: 'git clone https://github.com/Lakshya-2440/OpenScion.git && cd OpenScion && make install'
  };

  let activePlatform = 'darwin';

  installTabs.forEach(tab => {
    tab.addEventListener('click', () => {
      installTabs.forEach(t => t.classList.remove('active'));
      tab.classList.add('active');
      activePlatform = tab.dataset.platform || 'darwin';
      if (installCmdText) {
        installCmdText.textContent = INSTALL_COMMANDS[activePlatform];
      }
    });
  });

  if (btnCopyCmd) {
    btnCopyCmd.addEventListener('click', () => {
      const textToCopy = INSTALL_COMMANDS[activePlatform] || 'curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash';
      navigator.clipboard.writeText(textToCopy).then(() => {
        btnCopyCmd.classList.add('copied');
        btnCopyCmd.innerHTML = `
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <polyline points="20 6 9 17 4 12"></polyline>
          </svg> Copied!
        `;
        window.showToast('Install command copied to clipboard');

        setTimeout(() => {
          btnCopyCmd.classList.remove('copied');
          btnCopyCmd.innerHTML = `
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
            </svg> Copy Command
          `;
        }, 2200);
      });
    });
  }

  // 3. Command Pills on Terminal
  const cmdPills = document.querySelectorAll('.cmd-pill');
  cmdPills.forEach(pill => {
    pill.addEventListener('click', () => {
      const cmd = pill.dataset.cmd;
      if (cmd && window.runTerminalCommand) {
        window.runTerminalCommand(cmd);
      }
    });
  });

  // 4. Architecture Deep Dive Tabs
  const deepTabs = document.querySelectorAll('.deep-tab-btn');
  const deepContents = document.querySelectorAll('.deep-tab-content');

  deepTabs.forEach(btn => {
    btn.addEventListener('click', () => {
      deepTabs.forEach(b => b.classList.remove('active'));
      deepContents.forEach(c => c.classList.remove('active'));

      btn.classList.add('active');
      const targetId = btn.dataset.tab;
      const targetEl = document.getElementById(targetId);
      if (targetEl) targetEl.classList.add('active');
    });
  });

  // 5. Server Hub Config Generator
  const hubTabs = document.querySelectorAll('.hub-tab-btn');
  const hubCodeBlock = document.getElementById('hub-code-display');

  const HUB_CONFIGS = {
    local: `# Option A: Local Zero-Config Engine (Default SQLite WAL)
# 100% free, runs completely offline with embedded NVMe deduplication.

scion serve --port 8080`,

    minio: `# Option B: Free Distributed MinIO / S3 Storage Engine
# Ideal for self-hosted local network clusters and Docker containers.

export S3_ENDPOINT="127.0.0.1:9000"
export S3_BUCKET="openscion-blobs"
export S3_ACCESS_KEY="minioadmin"
export S3_SECRET_KEY="minioadmin"
export S3_USE_SSL="false"

scion serve --port 8080`,

    r2: `# Option C: Zero-Cost Cloudflare R2 ($0 Egress Fees)
# 10 GB free storage + 10M free requests per month.

export S3_ENDPOINT="<account-id>.r2.cloudflarestorage.com"
export S3_BUCKET="openscion-blobs"
export S3_ACCESS_KEY="<r2-access-key-id>"
export S3_SECRET_KEY="<r2-secret-access-key>"
export S3_USE_SSL="true"

scion serve --port 8080`,

    cockroach: `# Option D: Multi-Region PostgreSQL or CockroachDB Cluster
# Enterprise scale with distributed multi-region ACID transactions.

export DATABASE_URL="postgres://user:password@cockroach-cluster:26257/openscion?sslmode=require"
export S3_BUCKET="openscion-enterprise-blobs"

scion serve --port 8080 --db "$DATABASE_URL"`
  };

  hubTabs.forEach(tab => {
    tab.addEventListener('click', () => {
      hubTabs.forEach(t => t.classList.remove('active'));
      tab.classList.add('active');
      const key = tab.dataset.hub || 'local';
      if (hubCodeBlock) {
        hubCodeBlock.textContent = HUB_CONFIGS[key] || HUB_CONFIGS.local;
      }
    });
  });

  const btnCopyHub = document.getElementById('btn-copy-hub');
  if (btnCopyHub) {
    btnCopyHub.addEventListener('click', () => {
      const code = hubCodeBlock ? hubCodeBlock.textContent : '';
      navigator.clipboard.writeText(code).then(() => {
        window.showToast('Server configuration copied');
      });
    });
  }

  // 6. CLI Reference Filtering
  const cliFilterBtns = document.querySelectorAll('.cli-filter-btn');
  const cliCmdCards = document.querySelectorAll('.cli-cmd-card');

  cliFilterBtns.forEach(btn => {
    btn.addEventListener('click', () => {
      cliFilterBtns.forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      const filter = btn.dataset.filter || 'all';

      cliCmdCards.forEach(card => {
        if (filter === 'all' || card.dataset.category === filter) {
          card.style.display = 'block';
        } else {
          card.style.display = 'none';
        }
      });
    });
  });

  // Clicking a CLI command card copies its usage or triggers terminal
  cliCmdCards.forEach(card => {
    card.addEventListener('click', () => {
      const usageEl = card.querySelector('.cmd-card-usage');
      if (usageEl) {
        const text = usageEl.textContent.trim();
        if (window.runTerminalCommand) {
          window.runTerminalCommand(text);
          window.showToast(`Executed in terminal: ${text}`);
        }
      }
    });
  });

  // 7. Command Palette Modal (Cmd+K / Ctrl+K / '/' trigger)
  const cmdPaletteBackdrop = document.getElementById('cmd-palette-backdrop');
  const cmdPaletteInput = document.getElementById('cmd-palette-input');
  const cmdPaletteList = document.getElementById('cmd-palette-list');
  const searchTriggers = document.querySelectorAll('.search-trigger-btn');

  const PALETTE_ACTIONS = [
    { title: 'Run "scion status" in Terminal', category: 'Terminal', action: () => window.runTerminalCommand('scion status') },
    { title: 'Run "scion mount ./mnt" (0-byte VFS)', category: 'Terminal', action: () => window.runTerminalCommand('scion mount ./mnt') },
    { title: 'Run "scion commit --agent" in Terminal', category: 'Terminal', action: () => window.runTerminalCommand('scion commit -m "fix(auth): fast lease expiry" --agent claude-code-3.7-sonnet') },
    { title: 'Run "scion diff -s" in Terminal', category: 'Terminal', action: () => window.runTerminalCommand('scion diff -s') },
    { title: 'Run "scion log" in Terminal', category: 'Terminal', action: () => window.runTerminalCommand('scion log') },
    { title: 'Run Full Multi-Agent Demo Simulation', category: 'Simulation', action: () => window.runTerminalCommand('demo') },
    { title: 'Copy One-Line Install Command', category: 'Install', action: () => {
      navigator.clipboard.writeText('curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash');
      window.showToast('Copied curl install command');
    }},
    { title: 'Jump to Speculative Merge Queue Visualizer', category: 'Navigation', action: () => document.getElementById('merge-queue-section').scrollIntoView({ behavior: 'smooth' }) },
    { title: 'Jump to 0-Byte Ghost VFS Explorer', category: 'Navigation', action: () => document.getElementById('ghost-vfs-section').scrollIntoView({ behavior: 'smooth' }) },
    { title: 'Jump to Architectural Deep Dive', category: 'Navigation', action: () => document.getElementById('architecture-section').scrollIntoView({ behavior: 'smooth' }) },
    { title: 'Jump to Git vs OpenScion Comparison', category: 'Navigation', action: () => document.getElementById('comparison-section').scrollIntoView({ behavior: 'smooth' }) },
    { title: 'Jump to CLI Command Reference', category: 'Navigation', action: () => document.getElementById('cli-reference-section').scrollIntoView({ behavior: 'smooth' }) },
    { title: 'View OpenScion GitHub Repository', category: 'External', action: () => window.open('https://github.com/Lakshya-2440/OpenScion', '_blank') }
  ];

  function openPalette() {
    if (!cmdPaletteBackdrop) return;
    cmdPaletteBackdrop.classList.add('open');
    if (cmdPaletteInput) {
      cmdPaletteInput.value = '';
      renderPaletteList('');
      cmdPaletteInput.focus();
    }
  }

  function closePalette() {
    if (!cmdPaletteBackdrop) return;
    cmdPaletteBackdrop.classList.remove('open');
  }

  function renderPaletteList(query) {
    if (!cmdPaletteList) return;
    cmdPaletteList.innerHTML = '';

    const filtered = PALETTE_ACTIONS.filter(item => 
      item.title.toLowerCase().includes(query.toLowerCase()) || 
      item.category.toLowerCase().includes(query.toLowerCase())
    );

    if (filtered.length === 0) {
      cmdPaletteList.innerHTML = '<li style="padding:16px; color:#64748b; font-size:0.85rem; text-align:center;">No matching actions found</li>';
      return;
    }

    filtered.forEach((item, index) => {
      const li = document.createElement('li');
      li.className = index === 0 ? 'cmd-palette-item selected' : 'cmd-palette-item';
      li.innerHTML = `
        <span>${item.title}</span>
        <span class="kbd-shortcut">${item.category}</span>
      `;
      li.addEventListener('click', () => {
        closePalette();
        item.action();
      });
      cmdPaletteList.appendChild(li);
    });
  }

  searchTriggers.forEach(btn => {
    btn.addEventListener('click', openPalette);
  });

  if (cmdPaletteBackdrop) {
    cmdPaletteBackdrop.addEventListener('click', (e) => {
      if (e.target === cmdPaletteBackdrop) closePalette();
    });
  }

  if (cmdPaletteInput) {
    cmdPaletteInput.addEventListener('input', (e) => {
      renderPaletteList(e.target.value);
    });

    cmdPaletteInput.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        closePalette();
      } else if (e.key === 'Enter') {
        const selected = cmdPaletteList.querySelector('.cmd-palette-item.selected') || cmdPaletteList.querySelector('.cmd-palette-item');
        if (selected) {
          selected.click();
        }
      }
    });
  }

  // Global Keydown Listeners for shortcuts
  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      if (cmdPaletteBackdrop && cmdPaletteBackdrop.classList.contains('open')) {
        closePalette();
      } else {
        openPalette();
      }
    } else if (e.key === '/' && document.activeElement.tagName !== 'INPUT' && document.activeElement.tagName !== 'TEXTAREA') {
      e.preventDefault();
      openPalette();
    } else if (e.key === 'Escape') {
      closePalette();
    }
  });
})();
