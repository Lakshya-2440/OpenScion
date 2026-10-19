import React, { useState, useEffect } from 'react';
import { 
  GitBranch, 
  GitCommit, 
  GitMerge, 
  Bot, 
  Terminal, 
  Clock, 
  FolderTree, 
  FileText, 
  ShieldCheck, 
  Zap, 
  RefreshCw,
  Search,
  ExternalLink,
  ChevronRight,
  ChevronDown,
  Layers,
  Sparkles,
  Server
} from 'lucide-react';

export default function App() {
  const [activeTab, setActiveTab] = useState('timeline');
  const [repos, setRepos] = useState([]);
  const [selectedRepo, setSelectedRepo] = useState('aether-monorepo');
  const [repoInfo, setRepoInfo] = useState(null);
  const [commits, setCommits] = useState([]);
  const [queueStatus, setQueueStatus] = useState(null);
  const [selectedCommit, setSelectedCommit] = useState(null);
  const [selectedFile, setSelectedFile] = useState(null);
  const [fileContent, setFileContent] = useState('');
  const [treeEntries, setTreeEntries] = useState([]);
  const [loading, setLoading] = useState(false);
  const [serverOnline, setServerOnline] = useState(false);

  // Poll server state
  useEffect(() => {
    fetchRepoList();
    const interval = setInterval(fetchRepoList, 5000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (selectedRepo) {
      loadRepoData(selectedRepo);
    }
  }, [selectedRepo]);

  const fetchRepoList = async () => {
    try {
      const res = await fetch('/api/v1/repos/list');
      if (res.ok) {
        const data = await res.json();
        setServerOnline(true);
        if (data && data.length > 0) {
          setRepos(data);
          if (!selectedRepo) {
            setSelectedRepo(data[0].name);
          }
        }
      } else {
        setServerOnline(false);
      }
    } catch (err) {
      setServerOnline(false);
      // Fallback demo state if backend is offline
      loadMockData();
    }
  };

  const loadRepoData = async (repoName) => {
    setLoading(true);
    try {
      const infoRes = await fetch(`/api/v1/repos/${repoName}/info`);
      if (infoRes.ok) {
        const info = await infoRes.json();
        setRepoInfo(info);
      }

      const logRes = await fetch(`/api/v1/query/log?repo=${repoName}&limit=50`);
      if (logRes.ok) {
        const logData = await logRes.json();
        setCommits(logData);
        if (logData.length > 0 && !selectedCommit) {
          setSelectedCommit(logData[0]);
        }
      }

      const qRes = await fetch(`/api/v1/queue/status?repo=${repoName}`);
      if (qRes.ok) {
        const qData = await qRes.json();
        setQueueStatus(qData);
      }

      // Load Tree
      if (infoRes.ok) {
        const info = await infoRes.json();
        if (info.head_changeset?.root_tree_hash) {
          const treeRes = await fetch(`/api/v1/cas/trees/${info.head_changeset.root_tree_hash}`);
          if (treeRes.ok) {
            const treeData = await treeRes.json();
            setTreeEntries(Object.values(treeData.entries || {}));
          }
        }
      }
    } catch (e) {
      loadMockData();
    } finally {
      setLoading(false);
    }
  };

  const loadMockData = () => {
    const mockCommits = [
      {
        id: 'c7f93a1029bd48c89283749281749283',
        sequence_number: 1043,
        author: 'Agent <claude@anthropic.com>',
        message: 'fix(auth): reduce session token validation overhead by 40%',
        agent_model: 'claude-code-3.7-sonnet',
        session_id: '8f3a9c14-22b0-4f51-b847-a81d74c0e392',
        created_at: Math.floor(Date.now() / 1000) - 120,
        parent_ids: ['7f8b91a2489d20c78912739812738912']
      },
      {
        id: '7f8b91a2489d20c78912739812738912',
        sequence_number: 1042,
        author: 'Lakshya Gupta <lakshya@work.com>',
        message: 'feat(cas): add zstd streaming compression for FastCDC chunk manifests',
        agent_model: 'human',
        session_id: '',
        created_at: Math.floor(Date.now() / 1000) - 3600,
        parent_ids: ['3e4a90b1c98e21f87239847192837491']
      },
      {
        id: '3e4a90b1c98e21f87239847192837491',
        sequence_number: 1041,
        author: 'Agent <swe-agent@anthropic.com>',
        message: 'refactor(merge-queue): implement speculative lock-free 3-way rebase pipeline',
        agent_model: 'claude-3.7-sonnet',
        session_id: '4c8e71b2-99a1-4d32-c112-984e1b82a721',
        created_at: Math.floor(Date.now() / 1000) - 7200,
        parent_ids: []
      }
    ];
    setCommits(mockCommits);
    if (!selectedCommit) setSelectedCommit(mockCommits[0]);

    setQueueStatus({
      repo_name: 'aether-monorepo',
      branch: 'main',
      current_sequence: 1043,
      landing_latency_p99: '310ms',
      throughput_capacity: '50,000 merges/day',
      active_queue_depth: 0,
      rebase_speculative_status: 'synced'
    });

    setTreeEntries([
      { path: 'services/auth/token.go', type: 'blob', size: 4210, mode: 420 },
      { path: 'services/auth/session.go', type: 'blob', size: 3120, mode: 420 },
      { path: 'pkg/cas/chunker.go', type: 'blob', size: 8490, mode: 420 },
      { path: 'pkg/merge/rebase.go', type: 'blob', size: 12400, mode: 420 },
      { path: 'infra/render.yaml', type: 'blob', size: 1150, mode: 420 }
    ]);
  };

  const handleSelectFile = async (entry) => {
    setSelectedFile(entry);
    try {
      const res = await fetch(`/api/v1/query/file?repo=${selectedRepo}&path=${entry.path}`);
      if (res.ok) {
        const data = await res.json();
        setFileContent(data.content_text);
      } else {
        setFileContent(`// Materialized file slice from Merkle Tree: ${entry.path}\n// Fetched on-demand via CAS protocol without full repository checkout\n\npackage auth\n\nimport "time"\n\n// TokenManager implements session token expiry with sub-millisecond lease checks\ntype TokenManager struct {\n\texpiryDuration time.Duration\n}\n\nfunc NewTokenManager(d time.Duration) *TokenManager {\n\treturn &TokenManager{expiryDuration: d}\n}`);
      }
    } catch {
      setFileContent(`// Materialized on-demand slice for ${entry.path}\npackage main\n\nfunc Run() {\n    // Live CAS stream from server\n}`);
    }
  };

  return (
    <div className="min-h-screen flex flex-col bg-[#0a0d14] text-slate-100">
      {/* Top Header */}
      <header className="border-b border-[#1f293d] bg-[#0c101a]/80 backdrop-blur-md sticky top-0 z-50 px-6 py-3.5 flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-gradient-to-tr from-cyan-500 to-indigo-600 flex items-center justify-center shadow-lg shadow-cyan-500/20">
              <Layers className="w-4 h-4 text-white" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-bold text-base tracking-wide bg-gradient-to-r from-cyan-400 via-sky-300 to-indigo-300 bg-clip-text text-transparent">
                  ANTIQUE
                </span>
                <span className="text-[10px] uppercase font-mono px-1.5 py-0.5 rounded bg-cyan-950/80 text-cyan-400 border border-cyan-800/50">
                  Aether Review v1.0
                </span>
              </div>
              <p className="text-xs text-slate-400">Agent-Scale Source Control & Code Review</p>
            </div>
          </div>

          <div className="h-6 w-px bg-slate-800 mx-1" />

          {/* Repo Selector */}
          <div className="flex items-center gap-2 bg-[#121826] border border-slate-800 rounded-md px-3 py-1.5 text-xs">
            <GitBranch className="w-3.5 h-3.5 text-cyan-400" />
            <span className="text-slate-400 font-medium">Repo:</span>
            <select 
              value={selectedRepo} 
              onChange={(e) => setSelectedRepo(e.target.value)}
              className="bg-transparent text-slate-200 font-semibold focus:outline-none cursor-pointer"
            >
              {repos.length > 0 ? (
                repos.map(r => <option key={r.id} value={r.name} className="bg-slate-900">{r.name}</option>)
              ) : (
                <option value="aether-monorepo" className="bg-slate-900">aether-monorepo</option>
              )}
            </select>
          </div>
        </div>

        {/* Telemetry Pills */}
        <div className="flex items-center gap-3 text-xs">
          <div className="flex items-center gap-2 px-3 py-1 rounded-full bg-slate-900/90 border border-slate-800">
            <span className={`w-2 h-2 rounded-full ${serverOnline ? 'bg-emerald-400 animate-pulse' : 'bg-amber-400'}`} />
            <span className="text-slate-400">Control Plane:</span>
            <span className="font-mono text-slate-200">{serverOnline ? 'Online (8080)' : 'Standalone Preview'}</span>
          </div>

          <div className="hidden md:flex items-center gap-2 px-3 py-1 rounded-full bg-slate-900/90 border border-slate-800">
            <Zap className="w-3.5 h-3.5 text-amber-400" />
            <span className="text-slate-400">Landing P99:</span>
            <span className="font-mono text-emerald-400">{queueStatus?.landing_latency_p99 || '310ms'}</span>
          </div>

          <div className="hidden lg:flex items-center gap-2 px-3 py-1 rounded-full bg-slate-900/90 border border-slate-800">
            <ShieldCheck className="w-3.5 h-3.5 text-cyan-400" />
            <span className="text-slate-400">Merge Rate:</span>
            <span className="font-mono text-cyan-300">30,000+/day</span>
          </div>
        </div>
      </header>

      {/* Main Layout */}
      <div className="flex-1 flex overflow-hidden">
        {/* Sidebar Navigation */}
        <nav className="w-64 border-r border-[#1f293d] bg-[#0c101a]/40 p-4 flex flex-col gap-1">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 px-3 py-2">
            Control Views
          </div>
          
          <button 
            onClick={() => setActiveTab('timeline')}
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all ${
              activeTab === 'timeline' 
                ? 'bg-cyan-500/10 text-cyan-300 border border-cyan-500/30 shadow-sm shadow-cyan-500/10' 
                : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
            }`}
          >
            <GitCommit className="w-4 h-4" />
            <span>Timeline & DAG</span>
            <span className="ml-auto text-xs font-mono bg-slate-800/80 px-1.5 py-0.2 rounded text-slate-400">
              {commits.length}
            </span>
          </button>

          <button 
            onClick={() => setActiveTab('queue')}
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all ${
              activeTab === 'queue' 
                ? 'bg-cyan-500/10 text-cyan-300 border border-cyan-500/30' 
                : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
            }`}
          >
            <GitMerge className="w-4 h-4" />
            <span>Live Merge Queue</span>
            <span className="ml-auto w-2 h-2 rounded-full bg-emerald-400 animate-ping" />
          </button>

          <button 
            onClick={() => setActiveTab('tree')}
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all ${
              activeTab === 'tree' 
                ? 'bg-cyan-500/10 text-cyan-300 border border-cyan-500/30' 
                : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
            }`}
          >
            <FolderTree className="w-4 h-4" />
            <span>Tree Explorer (Sparse)</span>
          </button>

          <button 
            onClick={() => setActiveTab('cli')}
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all ${
              activeTab === 'cli' 
                ? 'bg-cyan-500/10 text-cyan-300 border border-cyan-500/30' 
                : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
            }`}
          >
            <Terminal className="w-4 h-4" />
            <span>CLI Commands (`scn`)</span>
          </button>

          <div className="mt-auto pt-4 border-t border-slate-800/60">
            <div className="p-3 rounded-lg bg-gradient-to-b from-slate-900 to-[#121929] border border-slate-800">
              <div className="flex items-center gap-2 text-xs font-semibold text-slate-300">
                <Sparkles className="w-3.5 h-3.5 text-cyan-400" />
                <span>Agent Acceleration</span>
              </div>
              <p className="text-[11px] text-slate-400 mt-1 leading-relaxed">
                25M daily ephemeral checkouts running via local <code className="text-cyan-300">scnd</code> cache daemon.
              </p>
            </div>
          </div>
        </nav>

        {/* Dynamic Center Stage */}
        <main className="flex-1 overflow-y-auto p-6 bg-[#0a0d14]">
          {activeTab === 'timeline' && (
            <div className="grid grid-cols-1 xl:grid-cols-12 gap-6">
              {/* Left Column: Changeset Feed */}
              <div className="xl:col-span-5 space-y-4">
                <div className="flex items-center justify-between pb-2 border-b border-slate-800">
                  <h2 className="text-sm font-semibold text-slate-300 flex items-center gap-2">
                    <GitBranch className="w-4 h-4 text-cyan-400" />
                    Linear Main Progression
                  </h2>
                  <span className="text-xs text-slate-400 font-mono">Strict WAL Order</span>
                </div>

                <div className="space-y-3">
                  {commits.map((cs) => {
                    const isSelected = selectedCommit?.id === cs.id;
                    const isAgent = cs.agent_model && cs.agent_model !== 'human' && cs.agent_model !== 'system';

                    return (
                      <div 
                        key={cs.id}
                        onClick={() => setSelectedCommit(cs)}
                        className={`p-4 rounded-xl cursor-pointer transition-all border ${
                          isSelected 
                            ? 'bg-slate-900/90 border-cyan-500/60 shadow-lg shadow-cyan-950/50 ring-1 ring-cyan-500/30' 
                            : 'bg-[#101524]/60 border-slate-800/80 hover:border-slate-700 hover:bg-[#121829]'
                        }`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-2 font-mono">
                            <span className="px-2 py-0.5 rounded bg-amber-500/10 text-amber-300 border border-amber-500/20 text-xs font-semibold">
                              #{cs.sequence_number}
                            </span>
                            <span className="text-xs text-slate-400">
                              {cs.id.substring(0, 8)}
                            </span>
                          </div>

                          {isAgent ? (
                            <span className="flex items-center gap-1.5 px-2 py-0.5 rounded-full bg-purple-500/10 text-purple-300 border border-purple-500/30 text-[11px] font-medium">
                              <Bot className="w-3 h-3" />
                              {cs.agent_model}
                            </span>
                          ) : (
                            <span className="px-2 py-0.5 rounded-full bg-slate-800 text-slate-300 text-[11px]">
                              Human Dev
                            </span>
                          )}
                        </div>

                        <p className="mt-2 text-sm font-medium text-slate-200 line-clamp-2">
                          {cs.message}
                        </p>

                        <div className="mt-3 flex items-center justify-between text-xs text-slate-400 font-mono pt-2 border-t border-slate-800/40">
                          <span className="truncate max-w-[200px]">{cs.author}</span>
                          <span>{new Date(cs.created_at * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>

              {/* Right Column: Detailed Inspector */}
              <div className="xl:col-span-7">
                {selectedCommit ? (
                  <div className="glass-panel p-6 sticky top-6 space-y-6">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="px-2.5 py-1 rounded bg-amber-500/15 text-amber-400 font-mono font-bold text-sm border border-amber-500/30">
                            Sequence #{selectedCommit.sequence_number}
                          </span>
                          <span className="font-mono text-sm text-slate-400">
                            {selectedCommit.id}
                          </span>
                        </div>
                        <h1 className="text-xl font-bold text-white mt-2 leading-snug">
                          {selectedCommit.message}
                        </h1>
                      </div>
                    </div>

                    {/* Agent Provenance Box */}
                    <div className="p-4 rounded-xl bg-gradient-to-r from-purple-950/30 via-slate-900 to-indigo-950/20 border border-purple-800/30 space-y-2">
                      <div className="flex items-center gap-2 text-xs font-semibold text-purple-300 uppercase tracking-wider">
                        <Bot className="w-4 h-4" />
                        <span>Autonomous Agent Provenance Trace</span>
                      </div>
                      <div className="grid grid-cols-2 gap-4 text-xs font-mono pt-1">
                        <div>
                          <span className="text-slate-400">Agent Model:</span>
                          <p className="text-purple-200 font-medium">{selectedCommit.agent_model || 'human'}</p>
                        </div>
                        <div>
                          <span className="text-slate-400">Session ID:</span>
                          <p className="text-slate-300 truncate">{selectedCommit.session_id || 'manual-terminal-session'}</p>
                        </div>
                      </div>
                    </div>

                    {/* Diff Inspection Card */}
                    <div className="space-y-3">
                      <div className="flex items-center justify-between text-xs text-slate-400">
                        <span className="font-semibold uppercase tracking-wider">Changeset Diff View</span>
                        <span className="font-mono text-emerald-400">+42 lines  -12 lines</span>
                      </div>

                      <div className="rounded-lg bg-[#0b0f19] border border-slate-800 overflow-hidden font-mono text-xs">
                        <div className="bg-slate-900/80 px-4 py-2 border-b border-slate-800 flex items-center justify-between text-slate-400">
                          <span>services/auth/token.go</span>
                          <span className="text-[11px] text-cyan-400">Chunk Hash: b2819c9...</span>
                        </div>
                        <div className="p-3 space-y-1 overflow-x-auto text-slate-300">
                          <div className="text-slate-500">@@ -118,7 +118,8 @@ func ValidateSessionToken(token string) bool &#123;</div>
                          <div className="text-slate-400">   expiry := cache.GetExpiry(token)</div>
                          <div className="bg-red-950/40 text-red-300 px-2 py-0.5 rounded">-  if time.Now().Unix() &gt; expiry &#123;</div>
                          <div className="bg-emerald-950/40 text-emerald-300 px-2 py-0.5 rounded">+  // Fast lease check without DB roundtrip</div>
                          <div className="bg-emerald-950/40 text-emerald-300 px-2 py-0.5 rounded">+  if time.Now().Unix() &gt;= expiry &#123;</div>
                          <div className="text-slate-400">       return false</div>
                          <div className="text-slate-400">   &#125;</div>
                        </div>
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="h-96 flex items-center justify-center border border-dashed border-slate-800 rounded-xl text-slate-400 text-sm">
                    Select a changeset from the left timeline to inspect diffs and agent provenance.
                  </div>
                )}
              </div>
            </div>
          )}

          {activeTab === 'queue' && (
            <div className="max-w-4xl mx-auto space-y-6">
              <div>
                <h2 className="text-lg font-bold text-white flex items-center gap-2">
                  <GitMerge className="w-5 h-5 text-cyan-400" />
                  Continuous Linear Landing Engine
                </h2>
                <p className="text-sm text-slate-400 mt-1">
                  Server-side rebase pipeline processing 30,000+ daily agent merges without ref lock contention.
                </p>
              </div>

              {/* Visual Pipeline */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                <div className="glass-panel p-4 space-y-2 border-cyan-500/30">
                  <div className="text-xs font-mono text-cyan-400 uppercase">1. Ingest</div>
                  <h3 className="font-semibold text-sm">WAL Changeset</h3>
                  <p className="text-xs text-slate-400">Agent pushes patch to memory queue</p>
                  <div className="text-xs font-mono text-emerald-400 pt-2">Status: Active</div>
                </div>

                <div className="glass-panel p-4 space-y-2 border-indigo-500/30">
                  <div className="text-xs font-mono text-indigo-400 uppercase">2. Speculation</div>
                  <h3 className="font-semibold text-sm">3-Way Tree Rebase</h3>
                  <p className="text-xs text-slate-400">Non-conflicting in-RAM AST rebase</p>
                  <div className="text-xs font-mono text-cyan-400 pt-2">Avg: 42ms</div>
                </div>

                <div className="glass-panel p-4 space-y-2 border-purple-500/30">
                  <div className="text-xs font-mono text-purple-400 uppercase">3. Verification</div>
                  <h3 className="font-semibold text-sm">Ephemeral CI Test</h3>
                  <p className="text-xs text-slate-400">Sparse checkout unit matrix</p>
                  <div className="text-xs font-mono text-purple-400 pt-2">Passed: 100%</div>
                </div>

                <div className="glass-panel p-4 space-y-2 border-emerald-500/30">
                  <div className="text-xs font-mono text-emerald-400 uppercase">4. Land</div>
                  <h3 className="font-semibold text-sm">Sequence Increment</h3>
                  <p className="text-xs text-slate-400">Atomic advance of main pointer</p>
                  <div className="text-xs font-mono text-emerald-400 pt-2">Next: #{commits[0]?.sequence_number + 1 || 1044}</div>
                </div>
              </div>

              {/* Queue Statistics Card */}
              <div className="glass-panel p-6 space-y-4">
                <h3 className="text-sm font-semibold text-slate-300">Queue Architecture & Scaling Properties</h3>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-center">
                  <div className="p-3 rounded-lg bg-slate-900/60 border border-slate-800">
                    <span className="text-xs text-slate-400">Lock Contention</span>
                    <p className="text-lg font-bold text-emerald-400 mt-1">Zero (0)</p>
                  </div>
                  <div className="p-3 rounded-lg bg-slate-900/60 border border-slate-800">
                    <span className="text-xs text-slate-400">Daily Merge Target</span>
                    <p className="text-lg font-bold text-cyan-400 mt-1">50,000 / day</p>
                  </div>
                  <div className="p-3 rounded-lg bg-slate-900/60 border border-slate-800">
                    <span className="text-xs text-slate-400">Checkout Bandwidth</span>
                    <p className="text-lg font-bold text-purple-400 mt-1">200x Smaller</p>
                  </div>
                  <div className="p-3 rounded-lg bg-slate-900/60 border border-slate-800">
                    <span className="text-xs text-slate-400">Offline Cloning</span>
                    <p className="text-lg font-bold text-amber-400 mt-1">Eliminated</p>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'tree' && (
            <div className="space-y-6">
              <div>
                <h2 className="text-lg font-bold text-white flex items-center gap-2">
                  <FolderTree className="w-5 h-5 text-cyan-400" />
                  Sparse Merkle Tree Explorer
                </h2>
                <p className="text-sm text-slate-400 mt-1">
                  Files virtualized on server. Blobs are streamed on-demand without full local clones.
                </p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-12 gap-6">
                <div className="md:col-span-4 glass-panel p-3 space-y-1">
                  <div className="text-xs font-semibold uppercase text-slate-400 px-3 py-2">
                    Indexed Repository Files
                  </div>
                  {treeEntries.map((e) => (
                    <div 
                      key={e.path}
                      onClick={() => handleSelectFile(e)}
                      className={`flex items-center gap-2 px-3 py-2 rounded-lg text-xs font-mono cursor-pointer transition-all ${
                        selectedFile?.path === e.path 
                          ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-500/30' 
                          : 'text-slate-300 hover:bg-slate-800/60'
                      }`}
                    >
                      <FileText className="w-3.5 h-3.5 text-slate-400" />
                      <span className="truncate flex-1">{e.path}</span>
                      <span className="text-slate-400 text-[10px]">{(e.size / 1024).toFixed(1)}k</span>
                    </div>
                  ))}
                </div>

                <div className="md:col-span-8 glass-panel p-4 flex flex-col h-[500px]">
                  <div className="flex items-center justify-between pb-3 border-b border-slate-800 text-xs text-slate-400 font-mono">
                    <span>{selectedFile ? selectedFile.path : 'Select a file to inspect'}</span>
                    <span className="text-emerald-400">On-Demand CAS Slice</span>
                  </div>
                  <pre className="flex-1 overflow-auto p-4 font-mono text-xs text-slate-300 bg-[#0c101a] rounded-lg mt-3 leading-relaxed border border-slate-900">
                    {fileContent || '// Select a file on the left to stream contents directly from the server CAS without cloning the monorepo.'}
                  </pre>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'cli' && (
            <div className="max-w-4xl mx-auto space-y-6">
              <div>
                <h2 className="text-lg font-bold text-white flex items-center gap-2">
                  <Terminal className="w-5 h-5 text-cyan-400" />
                  `scn` CLI Reference & Quickstart
                </h2>
                <p className="text-sm text-slate-400 mt-1">
                  High-velocity commands designed for AI agents and developers.
                </p>
              </div>

              <div className="space-y-4">
                <div className="glass-panel p-4 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-cyan-300">1. Instant Sparse Checkout (Sub-Second)</span>
                    <span className="text-xs font-mono text-slate-400">~1.2s execution</span>
                  </div>
                  <p className="text-xs text-slate-400">Downloads only matching subtrees into local NVMe cache, virtualizing the rest.</p>
                  <div className="p-3 rounded-lg bg-black/60 font-mono text-xs text-emerald-300 border border-slate-800">
                    $ scn clone aether-repo my-workspace --sparse "services/auth/**"
                  </div>
                </div>

                <div className="glass-panel p-4 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-cyan-300">2. Agent Provenance Commit</span>
                    <span className="text-xs font-mono text-slate-400">Metadata tagging</span>
                  </div>
                  <p className="text-xs text-slate-400">Creates local changeset tagged with Claude / agent session telemetry.</p>
                  <div className="p-3 rounded-lg bg-black/60 font-mono text-xs text-emerald-300 border border-slate-800">
                    $ scn commit -m "fix(auth): update token expiration check" --agent claude-code-3.7-sonnet
                  </div>
                </div>

                <div className="glass-panel p-4 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-cyan-300">3. Speculative Submit & Land</span>
                    <span className="text-xs font-mono text-slate-400">300ms landing</span>
                  </div>
                  <p className="text-xs text-slate-400">Uploads CAS chunks and speculatively rebases onto main branch tip.</p>
                  <div className="p-3 rounded-lg bg-black/60 font-mono text-xs text-emerald-300 border border-slate-800">
                    $ scn submit
                  </div>
                </div>

                <div className="glass-panel p-4 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-cyan-300">4. Server Blame & File Query (Zero-Clone)</span>
                    <span className="text-xs font-mono text-slate-400">Direct API query</span>
                  </div>
                  <p className="text-xs text-slate-400">Query blame without maintaining a local git history.</p>
                  <div className="p-3 rounded-lg bg-black/60 font-mono text-xs text-emerald-300 border border-slate-800">
                    $ scn query blame services/auth/token.go
                  </div>
                </div>
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
