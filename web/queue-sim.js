/**
 * OpenScion Speculative Merge Queue Simulator
 * Real-time visualization of Anthropic-class 50k+ merges/day rebase pipeline
 */

(function () {
  const streamContainer = document.getElementById('queue-agent-stream');
  const linearContainer = document.getElementById('queue-linear-main');
  const metricMerges = document.getElementById('metric-merges-count');
  const metricLatency = document.getElementById('metric-rebase-latency');
  const metricLocks = document.getElementById('metric-lock-wait');
  const metricThroughput = document.getElementById('metric-throughput');
  
  const btnPause = document.getElementById('btn-queue-pause');
  const btnInject = document.getElementById('btn-queue-inject');
  const btnBurst = document.getElementById('btn-queue-burst');

  if (!streamContainer || !linearContainer) return;

  let isPaused = false;
  let totalMerges = 52481;
  let currentSequence = 4813;
  let intervalId = null;

  const AGENTS = [
    { name: 'claude-code-worker-01', model: 'claude-3.7-sonnet', color: '#c084fc', icon: '🤖' },
    { name: 'cursor-agent-04', model: 'claude-3.7-sonnet', color: '#38bdf8', icon: '⚡' },
    { name: 'swe-bench-agent-09', model: 'deepseek-r1', color: '#34d399', icon: '🧠' },
    { name: 'devin-worker-02', model: 'agent-loop-v2', color: '#f59e0b', icon: '⚙️' },
    { name: 'lakshya-reviewer', model: 'human-dev', color: '#f43f5e', icon: '👤' }
  ];

  const SAMPLE_MSGS = [
    'fix(auth): reduce session token validation roundtrip to <2ms',
    'perf(cas): parallelize FastCDC boundary scan across 8 goroutines',
    'feat(vfs): implement sparse ghost tree directory pre-indexing',
    'refactor(queue): eliminate mutex lock on WAL head pointer',
    'test(merge): add AST conflict 3-way matrix test cases',
    'fix(storage): auto-fallback to Cloudflare R2 on MinIO timeout',
    'feat(cli): add short alias scn with colorized diffstat',
    'perf(sqlite): enable synchronous=NORMAL and WAL mode checkpoint',
    'fix(merkle): handle symlink path canonicalization on Linux arm64',
    'chore(ci): parallel checkout across 200 ephemeral runner pods'
  ];

  function getRandomItem(arr) {
    return arr[Math.floor(Math.random() * arr.length)];
  }

  function createChangeset(customMsg = null, customAgent = null) {
    const agent = customAgent || getRandomItem(AGENTS);
    const msg = customMsg || getRandomItem(SAMPLE_MSGS);
    const hash = Math.random().toString(16).substring(2, 10);
    const filesCount = Math.floor(Math.random() * 4) + 1;
    const rebaseMs = (Math.random() * 3 + 2).toFixed(1);

    return {
      agent,
      msg,
      hash,
      filesCount,
      rebaseMs,
      timestamp: new Date().toLocaleTimeString()
    };
  }

  function renderStreamItem(cs) {
    const item = document.createElement('div');
    item.className = 'agent-stream-item';
    item.innerHTML = `
      <div class="agent-item-header">
        <span class="agent-tag" style="color: ${cs.agent.color}">
          <span>${cs.agent.icon}</span> ${cs.agent.name}
        </span>
        <span class="ansi-dim">${cs.timestamp}</span>
      </div>
      <div class="agent-msg-text" title="${cs.msg}">${cs.msg}</div>
      <div style="display:flex; justify-content:space-between; margin-top:4px; font-size:0.7rem; color:#64748b; font-family:var(--font-mono)">
        <span>${cs.filesCount} file(s) staged</span>
        <span style="color:var(--cyan-primary)">speculative eval...</span>
      </div>
    `;

    streamContainer.insertBefore(item, streamContainer.firstChild);
    while (streamContainer.children.length > 6) {
      streamContainer.removeChild(streamContainer.lastChild);
    }

    // Trigger speculative rebase animation in middle panel
    triggerRebaseAnimation(cs);
  }

  function triggerRebaseAnimation(cs) {
    const walActive = document.getElementById('wal-active-block');
    if (walActive) {
      walActive.classList.add('processing');
      walActive.innerHTML = `
        <div style="display:flex; align-items:center; gap:8px;">
          <span class="pulse-dot"></span>
          <span>Speculative AST rebase: <strong>${cs.hash}</strong></span>
        </div>
        <span style="color:var(--cyan-primary); font-size:0.72rem;">${cs.rebaseMs}ms (0 lock wait)</span>
      `;

      setTimeout(() => {
        walActive.classList.remove('processing');
        landOnLinearMain(cs);
      }, 500);
    } else {
      landOnLinearMain(cs);
    }
  }

  function landOnLinearMain(cs) {
    currentSequence++;
    totalMerges++;

    if (metricMerges) metricMerges.textContent = totalMerges.toLocaleString();
    if (metricLatency) metricLatency.textContent = `${cs.rebaseMs} ms`;
    if (metricLocks) metricLocks.textContent = '0.00 ms (Lock-Free)';
    if (metricThroughput) metricThroughput.textContent = `${(51000 + Math.floor(Math.random() * 2000)).toLocaleString()}/day`;

    const item = document.createElement('div');
    item.className = 'linear-main-item';
    item.innerHTML = `
      <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:4px;">
        <span class="linear-seq-tag">#${currentSequence}</span>
        <span style="font-family:var(--font-mono); font-size:0.72rem; color:var(--emerald-primary);">✔ LANDED</span>
      </div>
      <div style="font-family:var(--font-mono); font-size:0.78rem; color:#fff; white-space:nowrap; overflow:hidden; text-overflow:ellipsis;">
        ${cs.msg}
      </div>
      <div style="display:flex; justify-content:space-between; margin-top:4px; font-size:0.7rem; color:#94a3b8; font-family:var(--font-mono);">
        <span style="color:${cs.agent.color}">${cs.agent.name}</span>
        <span class="ansi-dim">${cs.hash}</span>
      </div>
    `;

    linearContainer.insertBefore(item, linearContainer.firstChild);
    while (linearContainer.children.length > 6) {
      linearContainer.removeChild(linearContainer.lastChild);
    }
  }

  function scheduleNext() {
    if (isPaused) return;
    const delay = Math.floor(Math.random() * 1800) + 1200;
    intervalId = setTimeout(() => {
      const cs = createChangeset();
      renderStreamItem(cs);
      scheduleNext();
    }, delay);
  }

  // Button Listeners
  if (btnPause) {
    btnPause.addEventListener('click', () => {
      isPaused = !isPaused;
      btnPause.innerHTML = isPaused ? `
        <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg> Resume Stream
      ` : `
        <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg> Pause Stream
      `;
      if (!isPaused) scheduleNext();
    });
  }

  if (btnInject) {
    btnInject.addEventListener('click', () => {
      const custom = createChangeset('feat(agent): autonomous self-healing AST repair patch', AGENTS[0]);
      renderStreamItem(custom);
      if (window.showToast) window.showToast('Injected agent changeset into speculative rebase queue');
    });
  }

  if (btnBurst) {
    btnBurst.addEventListener('click', () => {
      if (window.showToast) window.showToast('Simulating concurrent burst of 5 AI agent checkouts...');
      for (let i = 0; i < 5; i++) {
        setTimeout(() => {
          const cs = createChangeset();
          renderStreamItem(cs);
        }, i * 200);
      }
    });
  }

  // Seed initial data
  for (let i = 0; i < 3; i++) {
    const cs = createChangeset();
    renderStreamItem(cs);
  }

  // Start loop
  scheduleNext();
})();
