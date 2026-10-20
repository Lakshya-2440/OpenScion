/**
 * OpenScion Terminal & Live Autonomous Agent Orchestrator
 * High-velocity terminal simulation of 50k+ merges/day agent workflows
 */

(function () {
  const terminalScreen = document.getElementById('terminal-screen');
  const termInput = document.getElementById('term-input');
  if (!terminalScreen || !termInput) return;

  const HISTORY = [];
  let historyIndex = -1;
  let currentSeq = 4814;
  let isStreamActive = true;
  let streamTimer = null;
  let activeTabMode = 'stream'; // 'stream' | 'interactive' | 'wal'

  // Autonomous Agent Pods Definition
  const AGENT_PODS = [
    {
      id: 0,
      name: 'claude-code-01',
      model: 'claude-3.7-sonnet',
      icon: '🤖',
      cardId: 'pod-card-0',
      progId: 'pod-prog-0',
      taskId: 'pod-task-0',
      statId: 'pod-stat-0',
      tasks: [
        { cmd: 'scion clone ./monorepo --sparse "services/auth/**"', output: '✔ Cloned 14 matching files in 3.1ms (virtualized 1.8M files at 0 bytes)', stat: '14 files • 3.1ms', note: '0-byte VFS' },
        { cmd: 'scion diff -s', output: 'Diffstat: 3 files changed, 48 insertions(+), 3 deletions(-)', stat: '3 files staged', note: 'FastCDC scan' },
        { cmd: 'scion commit -m "feat(auth): token lease validation" --agent claude-code-3.7-sonnet', output: '✔ [main #{SEQ} {HASH}] feat(auth): token lease validation (2.4ms, 18 chunks)', stat: 'WAL committed', note: '0 lock wait' },
        { cmd: 'scion blame services/auth/token.go', output: '#{SEQ} {HASH} (claude-code-01 2026-10-20) │ ValidateToken()', stat: 'provenance check', note: 'line blame' }
      ]
    },
    {
      id: 1,
      name: 'cursor-agent-04',
      model: 'cursor / claude-3.7',
      icon: '⚡',
      cardId: 'pod-card-1',
      progId: 'pod-prog-1',
      taskId: 'pod-task-1',
      statId: 'pod-stat-1',
      tasks: [
        { cmd: 'scion status', output: 'On branch main • Sequence #{SEQ} • 2 added, 1 modified', stat: 'working tree clean', note: 'branch synced' },
        { cmd: 'scion commit -m "fix(queue): non-blocking speculative rebase" --agent cursor', output: '✔ [main #{SEQ} {HASH}] Speculative AST rebase verified (2.1ms)', stat: 'AST verified', note: 'rebase 2.1ms' },
        { cmd: 'scion push origin main', output: '✔ Uploaded 6 FastCDC chunks • Landed sequence #{SEQ} in 3.4ms', stat: 'queue landed', note: '0 contention' },
        { cmd: 'scion sync', output: '✔ Fast-forward pull (0.6ms) ➜ Speculative push sequence #{SEQ} (3.8ms)', stat: 'sync complete', note: 'fast-forward' }
      ]
    },
    {
      id: 2,
      name: 'swe-bench-09',
      model: 'deepseek-r1 / agent',
      icon: '🧠',
      cardId: 'pod-card-2',
      progId: 'pod-prog-2',
      taskId: 'pod-task-2',
      statId: 'pod-stat-2',
      tasks: [
        { cmd: 'scion mount ./mnt', output: '✔ OpenScion VFS mounted in 4.8ms (254,820 ghost files, loopback WebDAV)', stat: '254k files', note: '0B on disk' },
        { cmd: 'scion commit -m "test(cas): verify rolling hash boundary" --agent swe-bench-09', output: '✔ [main #{SEQ} {HASH}] test(cas): boundary check (1.9ms)', stat: 'tests passed', note: 'WAL sequence' },
        { cmd: 'scion clone ./monorepo ./sandbox-test --sparse "pkg/cas/**"', output: '✔ Sparse sandbox materialized 8 files in 2.2ms', stat: '8 files hydrated', note: 'lazy fetch' },
        { cmd: 'scion log -n 3', output: '* #{SEQ} {HASH} (HEAD -> main) feat: speculative queue verified', stat: 'DAG timeline', note: 'linear graph' }
      ]
    },
    {
      id: 3,
      name: 'devin-worker-02',
      model: 'devin-loop-v2',
      icon: '⚙️',
      cardId: 'pod-card-3',
      progId: 'pod-prog-3',
      taskId: 'pod-task-3',
      statId: 'pod-stat-3',
      tasks: [
        { cmd: 'scion push', output: '✔ Pushed 1 changeset to origin: speculative AST rebase landed sequence #{SEQ}', stat: '0 lock wait', note: 'WAL landed' },
        { cmd: 'scion commit -m "chore(ci): parallelize checkout across 200 runner pods"', output: '✔ [main #{SEQ} {HASH}] 200 pods initialized (0 bytes bandwidth)', stat: '200 pods ready', note: 'egress $0' },
        { cmd: 'scion status -v', output: 'Branch main • Sequence #{SEQ} (head: {HASH}) • 0 dirty files', stat: 'clean tree', note: 'head verified' },
        { cmd: 'scion sync', output: '✔ Pull 0.8ms (fast-forward) ➜ Push landed sequence #{SEQ} (3.5ms)', stat: 'bidirectional sync', note: 'sub-second' }
      ]
    }
  ];

  function getTimestamp() {
    const d = new Date();
    return d.toTimeString().split(' ')[0];
  }

  function appendLine(htmlContent, isCommand = false) {
    const line = document.createElement('div');
    line.className = isCommand ? 'term-line cmd-echo' : 'term-line';
    line.innerHTML = htmlContent;
    terminalScreen.appendChild(line);

    // Limit screen lines to keep high performance
    while (terminalScreen.children.length > 80) {
      terminalScreen.removeChild(terminalScreen.firstChild);
    }
    terminalScreen.scrollTop = terminalScreen.scrollHeight;
  }

  function showBanner() {
    appendLine(`
<span class="ansi-bold ansi-white">   ___                   ____       _             </span>
<span class="ansi-bold ansi-white">  / _ \\ _ __   ___ _ __ / ___|  ___(_) ___  _ __  </span>
<span class="ansi-bold ansi-white"> | | | | '_ \\ / _ \\ '_ \\\\___ \\ / __| |/ _ \\| '_ \\ </span>
<span class="ansi-bold ansi-white"> | |_| | |_) |  __/ | | |___) | (__| | (_) | | | |</span>
<span class="ansi-bold ansi-white">  \\___/| .__/ \\___|_| |_|____/ \\___|_|\\___/|_| |_|</span>
<span class="ansi-bold ansi-white">       |_|                                        </span>
<span class="ansi-bold ansi-white">OpenScion v0.9.4 — Agent-Scale Terminal Version Control System</span>
<span class="ansi-dim">Architecture: Embedded FastCDC CAS + Lock-Free Speculative Rebase WAL</span>
<span class="ansi-dim">Mode: Autonomous Agent Swarm Stream Active • 4 Pods Orchestrating</span>
<span style="color:#22c55e;">● LIVE AGENT ORCHESTRATION FEED INITIALIZED — Streaming real-time commits:</span>
`);
  }

  // Autonomous Agent Stream Step
  function executeAgentStep() {
    if (!isStreamActive || activeTabMode === 'wal') return;

    const pod = AGENT_PODS[Math.floor(Math.random() * AGENT_PODS.length)];
    const taskObj = pod.tasks[Math.floor(Math.random() * pod.tasks.length)];
    const hash = Math.random().toString(16).substring(2, 10);
    currentSeq++;

    // Update HUD Cards
    AGENT_PODS.forEach(p => {
      const card = document.getElementById(p.cardId);
      if (card) card.classList.remove('active');
    });

    const activeCard = document.getElementById(pod.cardId);
    if (activeCard) activeCard.classList.add('active');

    const taskEl = document.getElementById(pod.taskId);
    if (taskEl) taskEl.textContent = taskObj.cmd;

    const statEl = document.getElementById(pod.statId);
    if (statEl) statEl.textContent = taskObj.stat.replace('{SEQ}', currentSeq).replace('{HASH}', hash);

    const progEl = document.getElementById(pod.progId);
    if (progEl) {
      progEl.style.width = '0%';
      setTimeout(() => { progEl.style.width = '100%'; }, 50);
    }

    const hudHead = document.getElementById('agent-hud-head');
    if (hudHead) hudHead.textContent = `#${currentSeq}`;

    // Terminal Logging
    const cmdFormatted = taskObj.cmd.replace('{SEQ}', currentSeq).replace('{HASH}', hash);
    const outFormatted = taskObj.output.replace('{SEQ}', currentSeq).replace('{HASH}', hash);
    const ts = getTimestamp();

    appendLine(`
<span class="ansi-dim">[${ts}]</span> <span class="ansi-bold">${pod.icon} ${pod.name}</span> <span style="color:var(--text-muted);">➜</span> <span class="ansi-bold ansi-white">${escapeHTML(cmdFormatted)}</span>
  <span style="color:#22c55e;">${escapeHTML(outFormatted)}</span>
`);
  }

  // Interactive Command Execution Handler
  function executeUserCommand(rawCmd) {
    const cmd = rawCmd.trim();
    if (!cmd) return;

    HISTORY.push(cmd);
    historyIndex = HISTORY.length;

    const ts = getTimestamp();
    appendLine(`
<span class="ansi-dim">[${ts}]</span> <span style="color:#fff; font-weight:700;">USER ➜</span> <span class="ansi-bold ansi-white">${escapeHTML(cmd)}</span>
`, true);

    const parts = cmd.split(/\s+/);
    let base = parts[0];
    let sub = parts[1] || '';

    if (base === 'scion' || base === 'scn') {
      base = sub;
      parts.shift();
    }

    switch (base.toLowerCase()) {
      case 'help':
      case '--help':
      case '-h':
        handleHelp();
        break;

      case 'status':
        handleStatus();
        break;

      case 'diff':
        handleDiff(cmd.includes('-s'));
        break;

      case 'commit':
        handleCommit(rawCmd);
        break;

      case 'mount':
        handleMount(parts.slice(1).join(' '));
        break;

      case 'clone':
        handleClone(parts.slice(1).join(' '));
        break;

      case 'log':
        handleLog();
        break;

      case 'blame':
        handleBlame();
        break;

      case 'push':
        handlePush();
        break;

      case 'pull':
      case 'sync':
        handleSync();
        break;

      case 'serve':
        handleServe();
        break;

      case 'clear':
        terminalScreen.innerHTML = '';
        break;

      case 'demo':
        triggerBurstSimulation(5);
        break;

      default:
        appendLine(`<span style="color:#ef4444;">✖ Unknown command "${escapeHTML(cmd)}". Type 'help' or click presets above.</span>`);
    }

    terminalScreen.scrollTop = terminalScreen.scrollHeight;
  }

  function handleHelp() {
    appendLine(`
<span class="ansi-bold ansi-white">OpenScion Core Commands:</span>
  <span class="ansi-white">status</span>           Working tree state and staged diffs
  <span class="ansi-white">diff [-s]</span>        Unified colorized diffs or summary diffstat
  <span class="ansi-white">commit -m &lt;msg&gt;</span>  Record atomic Merkle commit with AI model tag
  <span class="ansi-white">mount [dir]</span>       0-byte virtual ghost mount (on-demand chunk hydration)
  <span class="ansi-white">clone &lt;src&gt;</span>       Sub-second sparse monorepo clone
  <span class="ansi-white">log</span>               Linear DAG sequence numbers (#1, #2, #3...)
  <span class="ansi-white">push / pull</span>       Speculative lock-free merge queue synchronization
`);
  }

  function handleStatus() {
    currentSeq++;
    appendLine(`
<span class="ansi-dim">On branch </span><span class="ansi-bold ansi-white">main</span>  •  <span class="ansi-dim">Sequence </span><span class="term-badge-seq">#${currentSeq}</span>  <span class="ansi-dim">(head: 70c62899)</span>
<span class="ansi-dim">Agent Context:</span> <span class="term-badge-agent">🤖 claude-code-3.7-sonnet</span>

<span class="ansi-bold">Changes to be committed:</span>
  <span style="color:#22c55e;">+ services/auth/token_lease.go</span> (new file)
  <span style="color:#22c55e;">+ config/agent_runtime.json</span> (new file)
  <span style="color:#eab308;">~ pkg/queue/speculative.go</span> (modified)
<span class="ansi-dim">Summary: 3 files staged (FastCDC boundary scan verified)</span>
`);
  }

  function handleDiff(statOnly) {
    appendLine(`
<span class="ansi-bold">Diffstat (3 files):</span>
  services/auth/token_lease.go           │  42 <span style="color:#22c55e;">++++++++++++++++++++</span>
  config/agent_runtime.json              │  18 <span style="color:#22c55e;">++++++++++</span>
  pkg/queue/speculative.go               │  15 <span style="color:#22c55e;">++++++++</span><span style="color:#ef4444;">-------</span>
  <span class="ansi-dim">3 files changed, 68 insertions(+), 7 deletions(-)</span>
`);
  }

  function handleCommit(raw) {
    currentSeq++;
    const hash = Math.random().toString(16).substring(2, 10);
    appendLine(`
<span style="color:#22c55e; font-weight:700;">✔ [main #${currentSeq} ${hash}]</span> <span class="ansi-bold">fix(auth): fast lease expiry check</span>
   <span class="ansi-dim">3 file(s) changed  •  🤖 claude-code-3.7-sonnet</span>
   <span class="ansi-dim">Merkle Tree root: 5d18a2f901bc • Committed in 2.1ms (FastCDC deduplicated)</span>
`);
    const hudHead = document.getElementById('agent-hud-head');
    if (hudHead) hudHead.textContent = `#${currentSeq}`;
  }

  function handleMount(dir) {
    const target = dir || './mnt';
    appendLine(`
<span class="ansi-bold ansi-white">Mounting OpenScion Virtual Ghost Filesystem</span>
  • Mount point:        <span class="ansi-bold">${target}</span>
  • Revision:           <span class="term-badge-seq">#${currentSeq}</span> (HEAD)
  • Virtual Manifest:   254,820 entries in Merkle Tree
  • Disk Footprint:     <span style="color:#22c55e; font-weight:700;">0 Bytes</span> (hydrated on-demand)
  • Loopback WebDAV:    <span class="ansi-dim">Zero kernel extensions / No macFUSE required</span>
<span style="color:#22c55e; font-weight:700;">✔ VFS mounted successfully in 4.2ms!</span>
`);
  }

  function handleClone(argStr) {
    appendLine(`
<span class="ansi-bold ansi-white">Cloning monorepo into isolated agent pod...</span>
  • Sparse filter: <span class="ansi-dim">services/auth/**</span>
  • Virtualizing remainder of monorepo (1.8M files consume 0 bytes)
  • Hydrated 14 matching files in <span style="color:#22c55e; font-weight:700;">3.4ms</span>!
<span style="color:#22c55e; font-weight:700;">✔ Agent workspace ready!</span>
`);
  }

  function handleLog() {
    appendLine(`
<span class="ansi-bold">Linear Revision DAG (Strictly Sequential):</span>
  * <span class="term-badge-seq">#${currentSeq}</span> <span class="ansi-dim">70c62899</span> <span style="color:#22c55e;">(HEAD -> main)</span> fix: speculative AST rebase (2.1ms)
  * <span class="term-badge-seq">#${currentSeq - 1}</span> <span class="ansi-dim">3f82cd90</span> feat: loopback WebDAV zero kernel overhead
  * <span class="term-badge-seq">#${currentSeq - 2}</span> <span class="ansi-dim">9a7e4b1c</span> feat: FastCDC 64KB content-defined chunking
`);
  }

  function handleBlame() {
    appendLine(`
<span class="ansi-bold">Line-by-Line Provenance (services/auth/token.go):</span>
<span class="ansi-dim">#4810  9a7e4b1c  (Lakshya Gupta       2026-10-20)</span> │ package auth
<span class="ansi-dim">#4811  3f82cd90  (Claude Code Pod #01 2026-10-20)</span> │ func ValidateToken(token string) bool {
<span class="ansi-dim">#4812  70c62899  (SWE-bench Agent #09 2026-10-20)</span> │     return token != "" && !isRevoked(token)
<span class="ansi-dim">#4811  3f82cd90  (Claude Code Pod #01 2026-10-20)</span> │ }
`);
  }

  function handlePush() {
    currentSeq++;
    appendLine(`
Pushing to speculative rebase queue (origin)...
  <span style="color:#22c55e;">✔</span> In-memory AST diff verified (0 conflict)
  <span style="color:#22c55e;">✔</span> Landed strictly linearly at sequence <span class="term-badge-seq">#${currentSeq}</span> in 3.8ms (0 lock wait)!
`);
    const hudHead = document.getElementById('agent-hud-head');
    if (hudHead) hudHead.textContent = `#${currentSeq}`;
  }

  function handleSync() {
    handlePush();
  }

  function handleServe() {
    appendLine(`
<span class="ansi-bold ansi-white">OpenScion Central Team Hub & Merge Queue</span>
------------------------------------------------------------
• Endpoint:      <span style="color:#22c55e;">http://0.0.0.0:8080</span>
• Engine:        SQLite WAL (local zero-config)
• Throughput:    Speculative AST Lock-Free Queue (Max 50k/day)
• Connect:       scion remote add origin http://0.0.0.0:8080
------------------------------------------------------------
`);
  }

  function triggerBurstSimulation(count = 5) {
    appendLine(`<span style="color:#22c55e; font-weight:700;">⚡ SIMULATING CONCURRENT AGENT SWARM BURST (${count} AGENTS)...</span>`);
    for (let i = 0; i < count; i++) {
      setTimeout(() => {
        executeAgentStep();
      }, i * 350);
    }
  }

  function escapeHTML(str) {
    return str.replace(/[&<>'"]/g, 
      tag => ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        "'": '&#39;',
        '"': '&quot;'
      }[tag] || tag)
    );
  }

  // Input Listeners
  termInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      const val = termInput.value;
      termInput.value = '';
      executeUserCommand(val);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (historyIndex > 0) {
        historyIndex--;
        termInput.value = HISTORY[historyIndex] || '';
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (historyIndex < HISTORY.length - 1) {
        historyIndex++;
        termInput.value = HISTORY[historyIndex] || '';
      } else {
        historyIndex = HISTORY.length;
        termInput.value = '';
      }
    }
  });

  // Global trigger for pill buttons
  window.runTerminalCommand = function (cmdText) {
    if (termInput) {
      termInput.value = cmdText;
      executeUserCommand(cmdText);
    }
  };

  // Terminal Controls
  const btnPause = document.getElementById('btn-term-pause');
  if (btnPause) {
    btnPause.addEventListener('click', () => {
      isStreamActive = !isStreamActive;
      btnPause.textContent = isStreamActive ? 'Pause Stream' : 'Resume Stream';
      if (isStreamActive) {
        startStreamLoop();
        if (window.showToast) window.showToast('Resumed live agent orchestration stream');
      } else {
        clearInterval(streamTimer);
        if (window.showToast) window.showToast('Paused live stream');
      }
    });
  }

  const btnSwarm = document.getElementById('btn-term-swarm');
  if (btnSwarm) {
    btnSwarm.addEventListener('click', () => {
      triggerBurstSimulation(8);
      if (window.showToast) window.showToast('Simulating concurrent 8-agent swarm burst');
    });
  }

  const btnClear = document.getElementById('btn-term-clear');
  if (btnClear) {
    btnClear.addEventListener('click', () => {
      terminalScreen.innerHTML = '';
      showBanner();
    });
  }

  const btnCopy = document.getElementById('btn-term-copy');
  if (btnCopy) {
    btnCopy.addEventListener('click', () => {
      navigator.clipboard.writeText(terminalScreen.innerText).then(() => {
        if (window.showToast) window.showToast('Terminal log copied to clipboard');
      });
    });
  }

  // Terminal Tabs Switching
  const tabOrchestrator = document.getElementById('term-tab-orchestrator');
  const tabShell = document.getElementById('term-tab-shell');
  const tabWal = document.getElementById('term-tab-wal');

  function setTabMode(mode) {
    activeTabMode = mode;
    [tabOrchestrator, tabShell, tabWal].forEach(t => {
      if (t) t.classList.remove('active');
    });

    if (mode === 'stream' && tabOrchestrator) tabOrchestrator.classList.add('active');
    if (mode === 'interactive' && tabShell) tabShell.classList.add('active');
    if (mode === 'wal' && tabWal) tabWal.classList.add('active');

    if (mode === 'wal') {
      terminalScreen.innerHTML = '';
      appendLine(`<span class="ansi-bold ansi-white">--- STRICT LINEAR WRITE-AHEAD LOG (WAL) STREAM ---</span>`);
      for (let s = currentSeq - 6; s <= currentSeq; s++) {
        appendLine(`  WAL BLOCK #${s} │ STATUS: COMMITTED │ LATENCY: 2.1ms │ ZERO REF LOCK WAIT`);
      }
    } else if (mode === 'interactive') {
      appendLine(`<span class="ansi-dim">Switched to Interactive Shell. Click preset pills or type commands below.</span>`);
    } else {
      showBanner();
    }
  }

  if (tabOrchestrator) tabOrchestrator.addEventListener('click', () => setTabMode('stream'));
  if (tabShell) tabShell.addEventListener('click', () => setTabMode('interactive'));
  if (tabWal) tabWal.addEventListener('click', () => setTabMode('wal'));

  // Clicking an Agent Pod Card triggers a focused task in terminal
  document.querySelectorAll('.agent-pod-card').forEach(card => {
    card.addEventListener('click', () => {
      const agentId = parseInt(card.id.replace('pod-card-', ''), 10);
      const pod = AGENT_PODS[agentId];
      if (pod) {
        const task = pod.tasks[0];
        appendLine(`
<span class="ansi-bold ansi-white">Focus Pod: ${pod.name} (${pod.model})</span>
  Executing: ${task.cmd}
  ${task.output.replace('{SEQ}', currentSeq).replace('{HASH}', '70c62899')}
`);
        if (window.showToast) window.showToast(`Focused on ${pod.name} worker pod`);
      }
    });
  });

  // Start background streaming loop
  function startStreamLoop() {
    clearInterval(streamTimer);
    streamTimer = setInterval(() => {
      executeAgentStep();
    }, 2800);
  }

  // Initialize
  showBanner();
  startStreamLoop();
})();
