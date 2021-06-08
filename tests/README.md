# GPU operator test utilities

## Provided scripts / utilities

* `./tests/scripts/remote.sh [command]`: Execute `[command]` on the remote instance via SSH. This behaves the same as the `ssh` command and if no command is specified an interactive session is started.
* `./tests/scripts/remote.sh -t tmux new-session -A -s ${USER}`: Attach to (or create) a `tmux` session on the remote instance with a name matching the local user.