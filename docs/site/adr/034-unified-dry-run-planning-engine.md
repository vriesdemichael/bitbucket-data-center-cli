---
search:
  boost: 0.3
---

# ADR-034: Unified dry-run planning engine for server mutating commands

Use one planning engine for dry-run behavior across every command that changes state. A global --dry-run flag covers every such command, defaulting to stateful planning for server-mutating command handlers, with static planning previews retained as a compatibility and safety fallback and for commands that change only this machine.

Implement dry-run behavior through shared planning abstractions rather than per-command ad-hoc flags. Preview a command that changes this machine as well as one that changes the server; neither may make its change under --dry-run. Ensure dry-run output explicitly reports planning mode and capability signaling for each operation path.

A single planning model keeps previews from diverging between commands, improves operator trust, and keeps output behavior consistent across automation and interactive usage. Making stateful planning the primary implementation for server mutations improves preview quality, enables realistic no-side-effect validation against live Bitbucket state, and preserves a narrow static fallback for unsupported or future paths without redefining the main operator contract.

## Not chosen

- **Keep dry-run command-local**: Creates semantic drift, duplicated logic, and inconsistent output contracts.
- **Static-only dry-run globally**: Misses opportunities to provide stronger preflight confidence where API/state checks exist.
