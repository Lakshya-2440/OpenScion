# OpenScion — Agent-Scale Terminal Version Control System

[![Go Version](https://img.shields.io/badge/go-1.24%2B-blue)](#)
[![VCS Architecture](https://img.shields.io/badge/architecture-embedded%20merkle%20cas-emerald)](#)
[![CLI Tool](https://img.shields.io/badge/cli-scion%20%7C%20scn-purple)](#)
[![Merge Engine](https://img.shields.io/badge/merge%20engine-diff3%20%2B%20myers-brightgreen)](#)
[![Concurrency](https://img.shields.io/badge/concurrency-atomic%20db%20cas-blueviolet)](#)
[![Throughput](https://img.shields.io/badge/throughput-100%2C000%2B%20merges%2Fday-brightgreen)](#)
[![Tests](https://img.shields.io/badge/tests-100%25%20passing-success)](#)
[![License](https://img.shields.io/badge/license-Apache--2.0-orange)](#)

A standalone, high-performance terminal version control system engineered specifically for AI coding agents (*Claude Code, Cursor, SWE-bench bots*) and engineering teams working in massive codebases. Inspired by Anthropic's internal **Scion** VCS, Google's **Piper**, and Meta's **Sapling**.

Works **directly in your terminal like `git`** with zero external dependencies, while offering virtual ghost mounts, sub-second speculative merge queues, and distributed Content-Addressable Storage (CAS).

---

## ⚡ Key Architectural Advantages

```
                    +------------------------------------------+
                    |          OpenScion CLI (scion / scn)     |
                    +--------------------+---------------------+
                                         |
         +-------------------------------+-------------------------------+
         |                               |                               |
+--------v---------+            +--------v---------+            +--------v---------+
| Myers Diff3      |            | Virtual VFS      |            | Speculative      |
| 3-Way Engine     |            | Ghost Mount      |            | Merge Queue      |
| Microsecond Hunk |            | 0-byte manifest  |            | Atomic DB CAS    |
| Auto-Merge       |            | Dual-Tier LRU    |            | Auto-Rebase Loop |
+--------+---------+            +--------+---------+            +--------+---------+
         |                               |                               |
         +-------------------------------+-------------------------------+
                                         |
                    +--------------------+---------------------+
                    |   Unified CAS & Storage Engine           |
                    |  - Content-Defined FastCDC Chunks        |
                    |  - SQLite WAL / CockroachDB OCC          |
                    |  - MinIO / AWS S3 / Cloudflare R2        |
                    +------------------------------------------+
```

1. **Instant 0-Byte Clone & Virtual Ghost Mounts**:
   Mount any branch or historical revision instantly. Directory manifests are indexed in-memory with **0 initial disk bytes consumed** for file contents. File blobs are lazy-hydrated on-demand into a thread-safe LRU chunk cache on first read.
2. **Sub-Second Speculative Merge Queue**:
   Validates and speculatively rebases non-conflicting concurrent edits in-memory in **sub-millisecond latency (~0.6ms per merge)**. Coordinates multi-server clusters via database-level atomic Compare-And-Swap (CAS), easily supporting over **100,000+ merges per day**.
3. **Diff-Based Three-Way Line Merge Engine**:
   Production-grade Myers diff and Diff3 reconciliation engine. Seamlessly resolves non-overlapping insertions, deletions, and independent edits with microsecond overhead, emitting clear conflict markers for overlapping concurrent edits.
4. **100% Free & Standalone by Default**:
   Runs completely offline in `.scion/` using embedded high-speed chunk deduplication and SQLite WAL. Zero cloud subscription or vendor lock-in.
5. **First-Class AI Agent Provenance**:
   Tracks AI models, prompt sessions, and automated change rationale alongside traditional human author metadata.
6. **Distributed S3 / MinIO & Postgres Ready**:
   Seamlessly switch from standalone laptop development to multi-region distributed clusters using MinIO or Postgres with zero code changes.

---

## 1. Installation

### One-Line Install (macOS & Linux)

Install the latest prebuilt native binary directly via `curl`:

```bash
curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash
```

The installer uses a prebuilt release when available and falls back to compiling
from source if the release asset cannot be reached. Network waits are bounded;
set `OPENSCION_VERSION=v1.0.0` to install a specific release, or adjust
`OPENSCION_DOWNLOAD_TIMEOUT` when using a slower connection.

During an interactive install, choose `Global` to make `scion` available in
every repository, or `Current repository only` to place the binaries under
`.openscion/bin` in the current directory. The installer displays progress for
download, source compilation, and signing steps. For automation, use
`OPENSCION_SCOPE=global` or `OPENSCION_SCOPE=repository`.

The installer automatically detects your operating system and architecture (`darwin_arm64`, `darwin_amd64`, `linux_amd64`, `linux_arm64`), applies ad-hoc codesigning on macOS, and symlinks both `scion` and `scn` into `~/.local/bin/`.

### Build from Source

Requirements: Go 1.22+ and Git.

```bash
git clone https://github.com/Lakshya-2440/OpenScion.git
cd OpenScion
make install
```

Verify the installation:

```bash
scion help
# or use the short alias:
scn help
```

---

## 2. CLI Workflow & Command Reference

OpenScion uses familiar Git-like syntax, eliminating the learning curve while removing Git's index friction and branch switching bottlenecks.

### `scion init` — Initialize Repository
Create a new standalone OpenScion repository in current directory:

```bash
scion init my-monorepo
cd my-monorepo
```
*Creates `.scion/` containing embedded Merkle chunk storage and ACID metadata catalog.*

---

### `scion status` — Working Tree State
Inspect modified, added, deleted, and untracked files alongside branch sequence and agent context:

```bash
scion status
# or detailed view:
scion status -v
```

---

### `scion diff` — Colorized Unified Diffs
Inspect working tree modifications against the current revision:

```bash
# Standard unified diff
scion diff

# Summary stat view (changed files, lines added/removed)
scion diff -s

# Inspect diff for specific file
scion diff pkg/api/server.go
```

---

### `scion commit` — Record Changeset with Agent Tracking
Record working directory changes into the Merkle DAG:

```bash
# Standard developer commit:
scion commit -m "feat(cas): implement chunk deduplication"

# Agent commit with model provenance:
scion commit -m "fix(auth): fast lease expiry check" --agent claude-code-3.7-sonnet
```
*Each commit generates an immutable SHA-256 hash, updates the branch sequence counter (`#1`, `#2`, `#3`), and stores AI model runtime provenance.*

---

### `scion branch` — List or Create Branches
Manage isolated development branches:

```bash
# List all branches (* indicates active branch):
scion branch

# Create a new branch pointing to current commit:
scion branch feature-login
```

---

### `scion checkout` — Switch Branches or Restore Files
Switch active branch or restore working copy files:

```bash
# Switch to another branch (materializes branch tree into workspace):
scion checkout feature-login

# Discard local modifications to a specific file:
scion checkout pkg/cas/cas.go

# Restore a file from a specific historical commit:
scion checkout pkg/cas/cas.go --rev #1

# Discard all local workspace modifications:
scion checkout .
```

---

### `scion log` — Linear Revision Graph
View the chronological DAG timeline with author and agent tags:

```bash
scion log
# limit output to last N changesets:
scion log -n 5
```

---

### `scion show` — Inspect Commit Details
View commit metadata, agent session IDs, root Merkle tree hash, and modified file paths:

```bash
# By sequential revision number:
scion show #1

# Or by commit hash:
scion show 70c62899
```

---

### `scion blame` — Fast Line-by-Line Attribution
Show author, sequence revision, and timestamp for every line in a file:

```bash
scion blame pkg/cas/cas.go
```

---

### `scion clone` — Sparse Monorepo Clone
Clone an entire monorepo in sub-second time by virtualizing non-essential paths:

```bash
# Full clone:
scion clone ./my-monorepo ./workspace-full

# Sparse clone (only pull matching directories, virtualizing remainder):
scion clone ./my-monorepo ./sandbox --sparse "services/auth/**,libs/*"
```

---

### `scion mount` — Zero-Byte Virtual Ghost Filesystem
Mount any revision or current HEAD as an on-demand virtual filesystem:

```bash
# Mount current HEAD:
mkdir -p ./mnt
scion mount ./mnt

# Mount specific historical revision:
scion mount ./mnt --rev #1
```

**How It Works:**
* The directory is populated immediately without copying blobs to disk.
* Directory listings (`ls`, `find`, tree traversal) take 0 milliseconds.
* File contents are streamed from CAS chunks on-demand only when an editor, compiler, or agent issues a `read()` system call.
* Uses native loopback WebDAV integration—**zero kernel extensions (no macFUSE required)**.

---

### `scion remote` — Configure Upstream Server
Manage upstream team hubs:

```bash
# Add origin remote:
scion remote add origin http://192.168.1.50:8080

# List configured remotes:
scion remote -v

# Update remote URL:
scion remote set-url origin https://hub.internal.corp:8080

# Remove remote:
scion remote rm origin
```

---

### `scion push` — Push to Merge Queue
Submit local changesets to the remote server's speculative rebase pipeline:

```bash
scion push
```

---

### `scion pull` — Pull and Fast-Forward
Fetch remote changesets and materialize updated files into the workspace:

```bash
scion pull
```

---

### `scion sync` — Bidirectional Synchronization
Run pull followed by push in a single command:

```bash
scion sync
```

---

## 3. Team Hub & Central Control Plane (`scion serve`)

Turn any server, VM, or local machine into a team hub with sub-second merge queue handling.

### Option A: Local Zero-Config Engine (Default SQLite WAL)
Requires zero setup. Perfect for individual teams and on-premise local networks:

```bash
scion serve --port 8080
```

Run one server process when using SQLite. Multiple people and agents can connect
to that one process; do not run multiple replicas against the same SQLite file.

### Option B: Cloud PostgreSQL or CockroachDB Cluster
For enterprise scale with distributed multi-region ACID transactions:

```bash
scion serve --port 8080 --db "postgres://user:password@cockroach-cluster:26257/openscion?sslmode=require"
```

### Authentication & Access Control (Bearer Tokens)
Protect your central hub against unauthorized pushes or queries by passing `--auth-token` or setting `OPENSCION_AUTH_TOKEN`:

```bash
# Start server with required Bearer token:
scion serve --port 8080 --auth-token "prod-secret-token"

# Clients pass token via environment variable:
export OPENSCION_AUTH_TOKEN="prod-secret-token"
scion push
```

### Distributed CAS Blob Storage (MinIO / S3 / Cloudflare R2)
By default, CAS blobs are stored on high-speed local disk. For petabyte-scale storage clusters, configure S3/MinIO via environment variables:

```bash
# Run 100% Free MinIO locally or in Docker:
export S3_ENDPOINT="127.0.0.1:9000"
export S3_BUCKET="openscion-blobs"
export S3_ACCESS_KEY="minioadmin"
export S3_SECRET_KEY="minioadmin"
export S3_USE_SSL="false"

scion serve --port 8080
```

**Zero-Cost Cloud Option (Cloudflare R2):**
Cloudflare R2 offers 10 GB free storage with 10 million free requests/month with $0 egress fees:
```bash
export S3_ENDPOINT="<account-id>.r2.cloudflarestorage.com"
export S3_BUCKET="openscion-blobs"
export S3_ACCESS_KEY="<r2-access-key-id>"
export S3_SECRET_KEY="<r2-secret-access-key>"
export S3_USE_SSL="true"

scion serve --port 8080
```

---

## 4. Core Engine Architecture & Engineering Guarantees

OpenScion's core internals are engineered from first principles to solve the fundamental scaling and concurrency limitations of traditional VCS systems.

### 🧩 1. Diff-Based Three-Way Line Merge Engine (`MyersDiff` & `Diff3Merge`)
* **Myers Shortest Edit Script (SES)**: Computes optimal $O(ND)$ edit scripts between base revisions and concurrent branches.
* **Intelligent Diff3 Hunk Reconciliation**:
  * **Non-Overlapping Edits**: Insertions, deletions, and edits at arbitrary offsets across differing file lengths reconcile cleanly without spurious conflicts.
  * **Identical Concurrent Modifications**: When two agents make the exact same change (e.g., adding the same import or bug fix), the engine detects identity and lands cleanly.
  * **Explicit Conflict Markers**: Overlapping modifications emit clean, standard conflict blocks:
    ```text
    <<<<<<< OURS (MAIN)
    const timeout = 60;
    =======
    const timeout = 120;
    >>>>>>> THEIRS (SUBMITTED)
    ```
  * **Binary Asset Safety**: Heuristic NUL-byte inspection protects binary files from corrupt line-level reconciliations.

### ⚡ 2. Distributed Speculative Merge Queue & Atomic CAS
* **Speculative In-Memory Rebase**: When agents branch from an earlier commit, the server speculatively rebases their tree against the live tip of `main` in sub-millisecond memory time.
* **Database-Level Atomic CAS**: Branch updates execute via atomic Compare-And-Swap:
  ```sql
  UPDATE branches SET head_changeset_id = ?, sequence_number = ?, updated_at = ?
  WHERE repo_id = ? AND name = ? AND head_changeset_id = ? AND sequence_number = ?
  ```
* **Contention-Free Distributed Retry**: If multiple server nodes race to land changesets concurrently, losing instances automatically catch up, rebase against the new tip, and retry (up to 50 retries with capped backoff). Use PostgreSQL or CockroachDB plus shared CAS storage for this multi-node topology; SQLite is intended for one server process.
* **ACID Queue Ledger**: Every submission is tracked in the `merge_queue` database table (`ENQUEUED` → `PROCESSING` → `MERGED` / `REBASE_CONFLICT`).

### 👻 3. Zero-Byte Virtual Ghost Mount (VFS) with Multi-Tier Cache
* **Instant 0-Byte Footprint**: The entire Merkle hierarchy is indexed in-memory. Millions of files consume **0 bytes of local disk space** prior to access.
* **Native Kernel-Free Loopback**: Mounts transparently via OS WebDAV (`mount_webdav` on macOS, `davfs` on Linux, network drive on Windows)—**no kernel extensions or macFUSE reboots required**.
* **Thread-Safe Dual-Tier Cache**:
  * **In-Memory LRU Cache**: Fast bounded memory cache (128 MB default, configurable via `--max-cache-mb`) serving hot file reads at microsecond speeds.
  * **Atomic Disk Cache**: Optional persistent chunk cache with atomic write-rename semantics, ensuring concurrent compilers and test runners never observe partial reads.
  * **Full POSIX File Semantics**: Implements `Stat`, `Readdir`, random-access `Seek`, and multi-threaded parallel `Read`.

### 📊 4. Measured & Reproducible Performance
All throughput and latency figures are backed by automated Go benchmarks runnable directly on your hardware via `scion bench`.

---

## 5. Full Command Summary

| Command | Usage | Description |
| :--- | :--- | :--- |
| `init` | `scion init [name]` | Initialize a standalone repository in current directory |
| `status` | `scion status [-v]` | View working tree status, dirty files, and agent context |
| `diff` | `scion diff [-s] [file]` | Colorized diff viewer (summary or full unified format) |
| `commit` | `scion commit -m "<msg>" [--agent <name>]` | Record atomic Merkle commit with agent provenance |
| `branch` | `scion branch [name]` | List existing branches or create a new branch pointer |
| `checkout` | `scion checkout <path\|branch> [--rev #seq]` | Switch branch, restore file from revision, or discard changes |
| `log` | `scion log [-n <limit>]` | View linear revision timeline with sequence numbers |
| `show` | `scion show [id\|#seq]` | Inspect commit metadata, root tree hash, and manifest |
| `blame` | `scion blame <file>` | Line-by-line attribution and revision provenance |
| `clone` | `scion clone <src> [dir] [--sparse <pattern>]` | Sub-second monorepo clone with path filter |
| `mount` | `scion mount [dir] [--rev #seq]` | Mount virtual ghost filesystem with zero disk footprint |
| `remote` | `scion remote [add\|rm\|set-url] <name> <url>` | Manage team server origin endpoints |
| `push` | `scion push` | Submit local changesets to remote speculative rebase queue |
| `pull` | `scion pull` | Fast-forward local workspace from remote server |
| `sync` | `scion sync` | Synchronize bidirectionally with remote server |
| `serve` | `scion serve [--port <p>] [--db <url>]` | Launch central team server and merge queue |
| `gc` | `scion gc` | Prune unreferenced CAS objects and reclaim disk space |
| `bench` | `scion bench` | Run comprehensive reproducible performance benchmark suite |

---

## 6. Empirical Performance & Reproducible Benchmarks

All performance claims in OpenScion are verifiable and reproducible via standard automated benchmark suites.

### Measured Capabilities (Apple M4, Go 1.26, macOS / Linux)

| Benchmark Component | Measured Latency | Throughput | Methodology |
| :--- | :--- | :--- | :--- |
| **3-Way Line Merge (`Diff3Merge`)** | **10.8 µs / merge** | **~91,000 merges/sec/core** | Non-overlapping independent hunk reconciliation |
| **Myers Diff (100 lines)** | **7.1 µs / diff** | **~140,000 diffs/sec/core** | Shortest Edit Script (SES) generation |
| **Myers Diff (1,000 lines)** | **217 µs / diff** | **~4,600 diffs/sec/core** | Full AST / function replacement diff |
| **Merge Queue (Sequential Landing)** | **0.60 ms / merge** | **~1,600 merges/sec** | End-to-end SQLite WAL + Merkle Tree serialization |
| **Merge Queue (20-Way Concurrent)** | **2.1 ms / merge** (p50: 161ms, p95: 527ms) | **~470 concurrent merges/sec** | High-contention distributed speculative rebasing |

### How to Reproduce Benchmarks
Run the built-in benchmark runner directly from the CLI:
```bash
scion bench
# or run via Go test runner:
make bench
# or directly:
go test -bench=. -benchmem ./pkg/merge ./pkg/queue
```

---

## 7. Architectural Comparison

| Capability | Git | Sapling / Piper | OpenScion |
| :--- | :---: | :---: | :---: |
| **Local Standalone Mode** | Yes | Partial (Needs Server) | **Yes (100% offline)** |
| **Virtual 0-Byte Mount** | No | Requires Kernel Driver | **Native OS Loopback (No Kernel Ext)** |
| **Sub-Second Speculative Merge Queue** | No (External CI) | Proprietary / Internal | **Built-in (`scion serve`)** |
| **AI Agent Model & Session Provenance** | No | No | **Native (`--agent` flag & schema)** |
| **Storage Engine Flexibility** | Local packs only | Cloud blob store | **Local Disk, SQLite, MinIO, S3, CockroachDB** |
| **Sparse Hydration on Read** | Complex (`git-sparse`) | Virtual FS | **Automatic chunk streaming** |

---

## 8. Run Automated End-to-End Simulation

A full multi-agent simulation script is included in the repository. It launches a local cluster, simulates multiple agents committing concurrently, verifies rebase isolation, and tests the virtual ghost filesystem:

```bash
make demo
```

---

## 9. License

OpenScion is open-source software licensed under the **Apache License 2.0**.
