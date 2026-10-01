---
search:
  boost: 0.3
---

# ADR-090: A package sets up what every user shares, and bb sets up what is one user's

A package installs what reaches every user of the machine as files it owns: the bash, zsh and fish completion scripts, in the directories each shell reads for everyone. What lives in one user's files -- a PowerShell profile, a .zshrc, ~/.agents/skills -- is written by bb: `bb completion install` and `bb ai skill install`, each idempotent, each with a remove that takes out exactly what it wrote. A package that runs as its user after installing, which is Scoop's default, runs those commands and runs the removes before it uninstalls. A package that installs for every user, or can run nothing, names in its install message what each user runs. `bb completion install --all-users` writes where a shell reads for every user, where the shell has such a place, and refuses with the reason where it does not.

Do not make a package write into a user's home or edit a file the user owns; have it run or name the bb command that does. Add a shell or an operating system to `bb completion install` by asking the shell where it reads, as it asks PowerShell for its profile and zsh for its fpath. Where there is no such place, refuse and say so. What bb writes for a shell stays a loader that runs `bb completion <shell>`, so it follows upgrades, and checks bb is there first, so a shell starts quietly once bb is gone.

A package can own a file in a system directory; it cannot own a line in somebody's profile, and a machine-wide install that edits a profile edits whichever user ran it. Scoop is the exception because it installs per user and runs its hooks as that user. No location reaches every user's agents: the agents that read a machine-wide skills directory each read one of their own, and Claude Code's is its organization policy directory, whose skills override every user's own copy.

## Not chosen

- **Install the skill into each agent's machine-wide directory from the .deb and .rpm**: Three agent-specific paths to track, one of them a policy directory. Per-agent paths are left to the skills tooling (ADR-040).
- **Call the all-users flag --global**: bb ai skill install --global already means every project of one user.
