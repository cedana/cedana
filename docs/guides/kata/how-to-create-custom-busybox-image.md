# how-to-create-custom-busybox-image

Since `ctr` does not have any build commands, we will use the `docker CLI`. `ctr` provides an import command which can import images. The steps involve creating a Dockerfile. It is important to include `#!/bin/sh` as the first line of the file which is going to be the custom workload.

```bash
FROM busybox:latest
COPY <file to be copied> bin
```

Run these commands (might require sudo privileges) : This will import the custom workload inside the my-busybox image

```bash
docker build -t my-busybox .
docker save my-busybox > my-busybox.tar
ctr image import my-busybox.tar
```

The following commands give can be used to run the custom workload :

```bash
image="docker.io/library/my-busybox:latest"
sudo ctr run --runtime "io.containerd.kata.v2" --rm -t "$image" test-kata test.sh
```

We have a [cedana wrapper script](../../../scripts/kata-utils/cedana_kata_wrapper.c), which performs IO redirection of the workload. It is crucial because it enabled CRIU restore to work seamlessly. The executable compiled by building this wrapper, as well as the actual workload must be present inside the custom busybox image if we wish to perform checkpoint/restore.
