## CI Scripts

Self-explanatory, scripts to be run during CI, including benchmarking. 

## k8s

### `setup-host.sh` 

Sets up a node to run Cedana by installing prerequisites, copying binaries, and setting up the service.

### `cleanup-host.sh` 

Cleans up a node after Cedana is stopped.

### `bump-restart.sh` 

Restarts the Cedana service on the host. Without 

Not needed if running a Cedana AMI. 
