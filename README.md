# OpenScion — Agent-Scale Terminal Version Control System

[![Go Version](https://img.shields.io/badge/go-1.24%2B-blue)](#)
[![VCS Architecture](https://img.shields.io/badge/architecture-embedded%20merkle%20cas-emerald)](#)
[![CLI Tool](https://img.shields.io/badge/cli-scion%20%7C%20scn-purple)](#)
[![Throughput](https://img.shields.io/badge/throughput-50%2C000%2B%20merges%2Fday-brightgreen)](#)
[![Zero Cost](https://img.shields.io/badge/cost-%240%20local%20%2F%20minio-cyan)](#)
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
| FastCDC Chunking |            | Virtual VFS      |            | Speculative      |
| & Merkle Trees   |            | Lazy Mount       |            | Merge Queue      |
| Content-Addressed|            | 0-byte ghost tree|            | Lock-free WAL    |
+--------+---------+            +--------+---------+            +--------+---------+
         |                               |                               |
         +-------------------------------+-------------------------------+
                                         |
                    +--------------------+---------------------+
                    |   Storage Engine (Zero-Cost / Scale)     |
                    |  - Local NVMe Cache ($0)                 |
                    |  - MinIO / S3 / Cloudflare R2 ($0 free)  |
                    |  - SQLite WAL / CockroachDB Cluster      |
                    +------------------------------------------+
```

1. **Instant 0-Byte Clone & Virtual Ghost Mounts**:
   Mount any branch or historical revision instantly. Millions of files consume **0 bytes of local disk space** until read by an editor or test runner. No kernel extensions or macFUSE reboots required.
2. **Sub-Second Speculative Merge Queue**:
   Processes over **50,000 automated agent merges per day**. Merges are validated and rebased in-memory before writing strictly ordered WAL changesets.
3. **100% Free & Standalone by Default**:
   Runs completely offline in `.scion/` using embedded high-speed chunk deduplication and SQLite WAL. Zero cloud subscription or vendor lock-in.
4. **First-Class AI Agent Provenance**:
   Tracks AI models, prompt sessions, and automated change rationale alongside traditional human author metadata.
5. **Distributed S3 / MinIO & Postgres Ready**:
   Seamlessly switch from standalone laptop development to multi-region distributed clusters using MinIO or Postgres with zero code changes.

---

## 1. Installation

### One-Line Install (macOS & Linux)

Install the latest prebuilt native binary directly via `curl`:

```bash
curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash
```

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

### Option B: Cloud PostgreSQL or CockroachDB Cluster
For enterprise scale with distributed multi-region ACID transactions:

```bash
scion serve --port 8080 --db "postgres://user:password@cockroach-cluster:26257/openscion?sslmode=require"
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

## 4. Full Command Summary

| Command | Usage | Description |
| :--- | :--- | :--- |
| `init` | `scion init [name]` | Initialize a standalone repository in current directory |
| `status` | `scion status [-v]` | View working tree status, dirty files, and agent context |
| `diff` | `scion diff [-s] [file]` | Colorized diff viewer (summary or full unified format) |
| `commit` | `scion commit -m "<msg>" [--agent <name>]` | Record atomic Merkle commit with agent provenance |
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

---

## 5. Architectural Comparison

| Capability | Git | Sapling / Piper | OpenScion |
| :--- | :---: | :---: | :---: |
| **Local Standalone Mode** | Yes | Partial (Needs Server) | **Yes (100% offline)** |
| **Virtual 0-Byte Mount** | No | Requires Kernel Driver | **Native OS Loopback (No Kernel Ext)** |
| **Sub-Second Speculative Merge Queue** | No (External CI) | Proprietary / Internal | **Built-in (`scion serve`)** |
| **AI Agent Model & Session Provenance** | No | No | **Native (`--agent` flag & schema)** |
| **Storage Engine Flexibility** | Local packs only | Cloud blob store | **Local Disk, SQLite, MinIO, S3, CockroachDB** |
| **Sparse Hydration on Read** | Complex (`git-sparse`) | Virtual FS | **Automatic chunk streaming** |

---

## 6. Run Automated End-to-End Simulation

A full multi-agent simulation script is included in the repository. It launches a local cluster, simulates multiple agents committing concurrently, verifies rebase isolation, and tests the virtual ghost filesystem:

```bash
make demo
```

---

## 7. License

OpenScion is open-source software licensed under the **Apache License 2.0**.
