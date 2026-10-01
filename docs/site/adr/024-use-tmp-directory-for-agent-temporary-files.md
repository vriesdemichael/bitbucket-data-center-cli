---
search:
  boost: 0.3
---

# ADR-024: Use .tmp directory for agent temporary files

An agent writes its temporary files, such as downloads, response dumps, scratch output and intermediate artifacts, to `.tmp/` at the repository root and nowhere else: not a system temp directory, not the desktop, not a tracked source directory. Create `.tmp/` if it is missing, clear out what you no longer need, and keep `.tmp/.gitkeep`. The tasks write their own intermediate files there too, coverage profiles among them. A test is different: it works in a directory it created, normally `t.TempDir()`.

`.gitignore` ignores everything in `.tmp/` except `.gitkeep`, so nothing written there is committed by accident. A directory inside the repository is one an agent's sandbox lets it write, and one a person can find.
