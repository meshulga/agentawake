#!/bin/sh
# agentawake-pmset - privileged sleep toggle.
# Installed root:wheel mode 0755 at /usr/local/sbin/agentawake-pmset.
# Granted passwordless via /etc/sudoers.d/agentawake. Accepts ONLY 0 or 1.
case "$1" in
  0|1) exec /usr/bin/pmset -a disablesleep "$1" ;;
  *)   echo "usage: agentawake-pmset 0|1" >&2; exit 2 ;;
esac
