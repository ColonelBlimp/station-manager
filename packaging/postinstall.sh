#!/bin/sh
# RPM postinstall scriptlet. Runs as root, which is why we cannot do
# `systemctl --user enable` or `loginctl enable-linger` ourselves —
# both need to know which user, and the right answer is "the operator
# who'll run the daemon," not whoever invoked dnf. Print the next
# steps instead so the operator can run them in their own session.

cat <<'EOF'

Station Manager installed.

Next steps (run as your normal user, not root):

  systemctl --user daemon-reload
  systemctl --user enable --now smd

To start the daemon at boot without requiring a login session:

  loginctl enable-linger "$USER"

Defaults:

  URL:       http://127.0.0.1:8080
  Data dir:  ~/.local/share/station-manager/
  Logs:      ~/.local/share/station-manager/log/
  Config:    ~/.local/share/station-manager/config.json (created on first run)

EOF
