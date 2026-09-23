#!/bin/sh
# Runs after the .deb or .rpm installs or upgrades bb. The package puts
# completion for bash, zsh and fish where every user's shell reads it. The rest
# lives in each user's own files, which a package for the whole machine does
# not write -- there is no place for every user that several agents read a
# skill from -- so this names the commands instead.
#
# It only prints, and always exits 0: dpkg and rpm count a failing script as a
# failed installation.

# dpkg runs this with abort-upgrade and the like while it rolls a failed
# upgrade back, when there is nothing new to say.
case "$1" in
abort-*) exit 0 ;;
esac

cat <<'EOF'
bb: completion for bash, zsh and fish is installed. Each user sets up the rest:
  bb ai skill install --global               the agent skill, for coding agents
  bb completion install --shell powershell   completion in PowerShell (pwsh)
Run the skill install again after an upgrade, so the skill matches this bb.
EOF

exit 0
