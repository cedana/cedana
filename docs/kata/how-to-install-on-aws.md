This doc describes the steps to install kata-containers + containerd on AWS and running a workload. 

1. Since AWS does not support nested virtualization, we will have to use bare-metal instances.

2. For testing purposes, i3.metal instance type with Ubuntu 22.04 x86-64 AMI works the best. This combination is ideal for testing purposes. Allocate high amount of storage (64GB works)

3. Follow these steps to get the kata-containers release (steps borrowed from [this](https://github.com/kata-containers/kata-containers/blob/main/docs/install/container-manager/containerd/containerd-install.md) link) : 

    ```bash
    curl -O -L https://github.com/kata-containers/kata-containers/releases/download/3.5.0/kata-static-3.5.0-amd64.tar.xz
    tar -xvf kata-static-3.5.0-amd64.tar.xz
    sudo cp -r opt/ /
    sudo ln -s /opt/kata/bin/containerd-shim-kata-v2 /usr/bin
    sudo ln -s /opt/kata/bin/kata-runtime /usr/bin
    sudo ln -s /opt/kata/bin/kata-collect-data.sh /usr/bin
    sudo apt install containerd
    ```

4. Running a kata-container workload : 

    ```bash
    image="docker.io/library/busybox:latest"
    sudo ctr image pull "$image"
    sudo ctr run --runtime "io.containerd.kata.v2" --rm -t "$image" test-kata sh
    sudo ctr c ls
    ```

    This creates a container named `test-kata`, which runs the shell workload (specified using the “sh” at the end of the run command). The final ls command is to view the containers. The hypervisor used is QEMU. The file-system for the guest is read-only in the default setting (which uses images, instead of initrd, the distinction is made clear further in this document)

5. Attaching the debug-console to access the guest VM and exploring the kata-runtime options : 

    ```bash
    kata-runtime env
    ```

    This command gives information about the configuration file and the OS image used by the guest VM. We need to change the following field in

        Path : /opt/kata/share/defaults/kata-containers/configuration-qemu.toml
        debug_console_enabled = true

    Any containers created after enabling debug-console would have the facility of connecting with the guest VM. The command to connect with the guest VM is : 

        sudo kata-runtime exec test-kata