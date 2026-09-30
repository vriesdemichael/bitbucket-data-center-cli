---
search:
  boost: 0.3
---

# ADR-024: Use .tmp directory for agent temporary files

All AI agents use the .tmp/ directory at project root for temporary files, scratch work, and intermediate outputs. Agents do not use system temp paths or locations outside the repository root for temp artifacts.

Write temporary files (downloads, response dumps, generated scratch outputs, intermediate artifacts) to .tmp/ only. Create .tmp/ if missing. Do not use /tmp, %TEMP%, Desktop, or repository-tracked source directories. Clean up .tmp contents when no longer needed, but keep .tmp/.gitkeep.

Project-local temp storage avoids sandbox and permission issues with system temp locations, keeps debugging artifacts discoverable, and prevents accidental commits via .gitignore.
