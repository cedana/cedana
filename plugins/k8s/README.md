## Kubernetes Helper Plugin

Helper commands to setup inside of a Kubernetes environment.

### `cedana k8s-helper --setup-host`

Sets up a node to run Cedana by installing prerequisites, copying binaries, and setting up the service.

### `cedana k8s-helper destroy`

Cleans up a node after Cedana is stopped.

### `cedana k8s-helper --restart`

Restarts the Cedana service on the host.

Not needed if running a Cedana AMI.
