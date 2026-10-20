# Agent Version Control Protocol — OpenScion

This repository uses **OpenScion** (`scn` / `scion`) for version control, **NOT Git**.

## Core Protocol for AI Coding Agents
1. **Never run `git add` or `git commit`** inside repositories containing `.scion/`.
2. **Inspect Changes**: Run `scn status` or `scn status -v`.
3. **Inspect Diffs**: Run `scn diff` or `scn diff -s`.
4. **Record Changesets**: Run `scn commit -m "<concise message>" --agent <your-agent-name>`
   - Example: `scn commit -m "fix(cas): handle corrupted chunks" --agent claude-code-3.7-sonnet`
5. **View Timeline**: Run `scn log` or `scn log -n 5`.
6. **Inspect Commits**: Run `scn show #1` or `scn show <commit-id>`.
7. **Synchronize**: Run `scn sync` (or `scn pull` / `scn push`).

## Fast Troubleshooting
- If remote is not configured: check `.scion/config.json` or run `scn remote add origin <url>`.
- To mount virtual filesystem: `scn mount ./mnt`.
