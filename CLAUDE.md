# Claude Code Instructions — OpenScion VCS

This project uses **OpenScion** (`scn`) for version control.

## Guidelines
- Do not use Git commands (`git add`, `git commit`).
- Use `scn status` to check modified files.
- Use `scn diff` to verify code edits.
- Use `scn commit -m "<message>" --agent claude-code-3.7-sonnet` to save changesets.
- Use `scn log` to review recent changes.
