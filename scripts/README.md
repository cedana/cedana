## ci 
Self-explanatory, scripts to be run during CI, including benchmarking. 

## k8s 
### `setup-host.sh` 
Sets up a node to run Cedana by installing prereqs. Not needed if running a Cedana AMI. 

### `chroot-start.sh` 
Starts the daemon after `chroot`ing into `/host`. 
