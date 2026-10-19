# OpenScion — Agent-Scale Terminal Version Control System

[![Go Version](https://img.shields.io/badge/go-1.24%2B-blue)](#)
[![VCS Architecture](https://img.shields.io/badge/architecture-embedded%20merkle%20cas-emerald)](#)
[![CLI Tool](https://img.shields.io/badge/cli-scion-purple)](#)
[![Throughput](https://img.shields.io/badge/throughput-sub--second%20landing-brightgreen)](#)

A standalone, high-performance terminal version control system built from the ground up for developer and AI-agent workflows. Inspired by Anthropic's internal **Scion** VCS and Google's **Piper** / Meta's **Sapling**.

Works **directly in the terminal like `git`** with zero external servers or background daemons required for local repositories.

---

## 1. Quick Install

Install in one command with `curl`:

```bash
curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash
```

*(Or compile locally with `make install`)*

Verify installation:
```bash
scion --help
```


---

## 2. Using `scion` (Git-like Terminal Workflow)

### Initialize a Repository
```bash
# In any project directory:
scion init my-project
```
Creates a `.scion/` directory containing embedded Merkle Content Addressable Storage (CAS) and an ACID SQLite metadata DAG.

### Check Status
```bash
scion status
```
Outputs an ANSI terminal card showing modified, added, and deleted files alongside linear sequence numbers and AI agent runtime context.

### View Colorized Diffs
```bash
scion diff
```
Shows unified diffs directly in terminal with green additions and red deletions.

### Commit Changes with Agent Provenance
```bash
# Standard commit
scion commit -m "feat: implement token validation"

# Or with AI Agent tracking:
scion commit -m "fix(auth): fast lease expiry check" --agent claude-code-3.7-sonnet
```
Automatically captures agent model name, session context, and Merkle tree root hash.

### View Linear Sequence Graph
```bash
scion log
```
Prints the commit timeline with linear sequence numbers (`#1`, `#2`, `#3`), short hashes, author, agent tags, and timestamps.

### Line-by-Line Blame
```bash
scion blame src/index.ts
```

### Sub-Second Sparse Clone
```bash
# Clone only matching paths into a workspace, virtualizing the rest:
scion clone ./my-project ./agent-sandbox --sparse "services/auth/**"
```

### Virtual Filesystem Mount (Zero-Byte Ghost Drive)
```bash
# Mount current HEAD or any commit as a virtual filesystem:
scion mount ./mnt

# Or mount specific historical commit:
scion mount ./mnt --rev #1
```
*Instant 0-byte disk footprint. Files stream from CAS chunks on-demand only when opened.*

### Inspect Commits
```bash
scion show #1
```

---

## 3. Command Reference

| Command | Description |
| :--- | :--- |
| `scion init [name]` | Initialize a standalone OpenScion repository in current directory. |
| `scion status` | Display working tree status, dirty files, and agent context. |
| `scion diff` | Inspect colorized unified diffs against HEAD. |
| `scion commit -m "<msg>"` | Commit changes to local DAG with agent provenance. |
| `scion log` | View commit timeline with linear sequence numbers. |
| `scion blame <path>` | Line-by-line blame with sequence numbers and authors. |
| `scion show [commit-id\|#seq]` | Display commit details, tree hashes, and file lists. |
| `scion mount [dir] [--rev #seq]` | Mount virtual ghost filesystem (zero bytes on disk, lazy-hydrated on read). |
| `scion clone <src> [dir]` | Fast sparse clone from local path or remote repository. |
| `scion serve [--port 8080]` | Optional: launch central hub server for multi-agent network teams. |

---

## 4. Run Automated End-to-End Simulation

Test the complete system in action:
```bash
make demo
```
