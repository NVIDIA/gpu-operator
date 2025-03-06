# GPU operator test utilities

## Testing locally
The `local.sh` script allows for triggering basic end-to-end testing of the GPU
operator from a local machine.

Running the command:
```bash
./tests/local.sh
```
will perform the following steps:
1. Launch an AWS instance using the `aws-kube-ci` subrepo
1. Sync the project folder to the newly created instance.
1. Install additional prerequsites (see `scripts/prerequisites.sh`)
1. Execute the default test case (`cases/defaults.sh`) on the remote instance

If an instance has already been launched with terraform and the launch phase is to be skipped, the following can be used:
```
SKIP_LAUNCH=true ./tests/local.sh
```
For documentation on configuring instance creation, see the associated [README](../aws-kube-ci/README.md) folder.

### Cleaning up
If the tests succeed, the operator and workloads should be uninstalled (`scripts/uninstall.sh`). If the tests fail there may be state that needs to be cleaned up. This can be confirmed by running `kubectl` commands on the remote instance:

For example:
```bash
./tests/scripts/remote.sh kubectl get pods -A
./tests/scripts/remote.sh kubectl get namespace
```

Which would show the pods associated with the `gpu-operator-resources` namespace and or the `test-operator` namespace.

Running:
```bash
CLEANUP=true ./tests/local.sh
```
should cleanup the resources on the remote machine to allow the tests to be run again.

## Provided scripts / utilities

* `./tests/scripts/remote.sh [command]`: Execute `[command]` on the remote instance via SSH. This behaves the same as the `ssh` command and if no command is specified an interactive session is started.
* `./tests/scripts/remote.sh -t tmux new-session -A -s ${USER}`: Attach to (or create) a `tmux` session on the remote instance with a name matching the local user.