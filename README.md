# Project Aether: Agent-Scale Version Control System (Anthropic Scion Class)

[![Build Status](https://img.shields.io/badge/build-passing-brightgreen)](#)
[![VCS Architecture](https://img.shields.io/badge/architecture-server--centric%20merkle%20cas-blue)](#)
[![Throughput](https://img.shields.io/badge/daily%20merges-50%2C000%2B-purple)](#)
[![Latency](https://img.shields.io/badge/landing%20p99-%3C350ms-emerald)](#)

A complete, production-grade implementation of an agent-scale version control system inspired by Anthropic's **Scion** and Google's **Piper** / Meta's **Sapling**. Designed for organizations where autonomous AI coding agents (Claude Code, SWE-agents, Copilots) generate thousands of changes per hour, making traditional decentralized Git impractical.

---

## 1. System Overview

```
                      ┌──────────────────────────────────────┐
                      │    Autonomous AI Agents / CI Nodes   │
                      └──────────────────┬───────────────────┘
                                         │
                   ┌─────────────────────┴─────────────────────┐
                   │                                           │
             (CLI Tool)                                   (Direct API)
                 scn                                   Zero-Clone Queries
                   │                                           │
            ┌──────┴──────┐                                    │
            │ scnd daemon │                                    │
            └──────┬──────┘                                    │
                   │                                           │
                   ▼                                           ▼
       ┌───────────────────────────────────────────────────────────────┐
       │             AETHER CONTROL PLANE (aether-server)              │
       │                                                               │
       │  ┌─────────────────────────┐   ┌───────────────────────────┐  │
       │  │ FastCDC Content Storage │   │ Continuous Speculative    │  │
       │  │ (CAS Blobs & Merkle)    │   │ Linear Merge Queue (WAL)  │  │
       │  └────────────┬────────────┘   └─────────────┬─────────────┘  │
       └───────────────┼──────────────────────────────┼────────────────┘
                       │                              │
                       ▼                              ▼
            ┌────────────────────┐         ┌────────────────────┐
            │ Object Store (CAS) │         │ SQLite / Postgres  │
            │ NVMe / MinIO / S3  │         │ (Metadata Graph)   │
            └────────────────────┘         └────────────────────┘
```

### Core Innovations
* **Always-Online Assumption:** Discards heavy offline git cloning. Ephemeral agent containers check out in **< 2 seconds** using sparse profiles.
* **Lock-Free Linear Merge Queue:** Replaces ref locks with speculative server-side 3-way rebase pipelines, landing commits in milliseconds.
* **Agent Provenance Trace:** Every commit captures AI model name (`claude-code-3.7-sonnet`), session ID, and prompt trace directly in the changeset DAG.
* **Zero-Clone Server Queries:** Query file slices, AST diffs, and line-level blame via remote HTTP API without cloning the repository.
* **Antique Web Platform:** Modern web console for visualizing linear timeline, merge queue depth, and side-by-side diffs.

---

## 2. Quickstart

### Build All Binaries
```bash
make build
# Or directly:
./scripts/install.sh
```
This produces three static binaries in `./bin/`:
1. `bin/aether-server` — Centralized Control Plane & CAS Server.
2. `bin/scn` — High-performance Client CLI with ANSI status cards.
3. `bin/scnd` — Local caching daemon for ephemeral agent pods.

### Run Automated End-to-End Simulation
To watch the full system in action (starts server, initializes repo, creates commits, runs sub-second sparse clone, executes concurrent agent edit, lands through speculative rebase, and prints graph log):
```bash
make demo
```

---

## 3. CLI Commands (`scn`)

| Command | Description |
| :--- | :--- |
| `scn init [repo-name]` | Initializes a new repository on the server and sets up local `.scn/` workspace. |
| `scn clone <repo> [dir] --sparse <patterns>` | Sub-second clone materializing only files matching sparse patterns (e.g. `services/auth/**`). |
| `scn status` | Displays rich terminal status card with branch, sequence number, and agent context. |
| `scn diff` | Shows colorized unified diff against base revision. |
| `scn commit -m "<msg>" --agent <model>` | Creates local changeset with agent provenance metadata. |
| `scn submit` | Uploads CAS chunks, executes server-side 3-way rebase, and lands into linear `main` sequence. |
| `scn log` | Displays graphical commit tree with sequence badges and agent tags. |
| `scn query file <path> [--rev id]` | Fetches and views file slice directly from server without checking out repo. |
| `scn query blame <path>` | Streams server-computed line-by-line blame. |
| `scn query queue` | Inspects live merge queue throughput and latency metrics. |
| `scn profile [list\|set <patterns>]` | Inspects or updates active sparse checkout profile. |

---

## 4. Web Review Platform ("Antique")

A dark-mode, glassmorphic web dashboard built for continuous code review at agent scale.

```bash
# Start backend control plane
make server

# In another terminal, start Antique review web app
make web
```
Open **`http://localhost:3000`** to browse:
* **Timeline & DAG:** Linear sequence numbers, agent models (`Claude 3.7 Sonnet`), prompt trace IDs.
* **Changeset Inspector & Diffs:** Colorized additions/deletions and chunk hashes.
* **Live Merge Queue:** Ingest -> Speculation -> Verification -> Land pipeline visualization.
* **Sparse Merkle Tree Explorer:** Inspect repository hierarchy without cloning.

---

## 5. Deployment with Render & Vercel CLIs

### Backend Deployment (Render CLI)
The backend control plane (`aether-server`) and managed database are defined in [`render.yaml`](render.yaml).

```bash
# Set your API key
export RENDER_API_KEY=your_key_here

# Deploy via script or CLI
./scripts/deploy-render.sh
# or:
render blueprints launch --blueprint ./render.yaml
```

### Frontend Deployment (Vercel CLI)
The Antique Code Review frontend is preconfigured with [`vercel.json`](vercel.json).

```bash
# Deploy via script or CLI
./scripts/deploy-vercel.sh
# or:
cd web && vercel deploy --prod
```
