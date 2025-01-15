## Checkpoint/Restore kata containers
> [!WARNING]
> This feature is still experimental.

This doc describes the steps that have to be performed to save-migrate-resume a kata-container workload. It includes building a CRIU compatible kernel and rootfs, and a busy-box image which includes the cedana wrapper. These steps are also exactly what are performed by an end-to-end test for kata container workloads which are checkpointed and restored using cedana. 

1. Get the [fork of kata-containers](https://github.com/cedana/kata-containers), created under cedana. Switch to the rootfs-kernel branch. 
2. Create the kernel compatible with CRIU : Steps documented [here](./how-to-make-kernel-criu-compatible.md)
3. Create a CRIU compatible rootfs and the corresponding image : Steps documented [here](./how-to-make-rootfs-criu-compatible.md)
4. Create a custom busybox image with the cedana wrapper and the workload : Steps documented [here](./how-to-create-custom-busybox-image.md)
5. Copy the [config file](../../scripts/kata-utils/configuration-qemu.toml) at the correct location `/opt/kata/share/defaults/kata-containers/`
6. Run the workload: 

    ```bash
    image=docker.io/library/my-busybox:latest
    sudo ctr run --runtime "io.containerd.kata.v2" --rm -t "$image" test_vm cedana test.sh
    ```

7. Log into the VM: 

    ```bash
    sudo kata-runtime exec test_vm
    ```

8. Run the installation script inside the VM: 

    ```bash
    root@localhost:/# build_start_daemon.sh
    ```

9. Create a checkpoint from the host: The “-d” flag corresponds to the path on the host where we want to save the checkpoint tar file. The file is saved as “dmp.tar”.  

    ```bash
    ./cedana dump kata test_vm -d .
    ```

    The mandatory argument is the VM name (test_vm) in our case. This command saves the tar file in the same folder as the cedana binary, as we have provided the argument as “.”

10. Run a new VM, with any dummy workload. Log into the VM and run the installation scripts. Now, we can restore the previous workload into the new VM. Again, the “-d” flag corresponds to the path on the host of the tar file. 

    ```bash
    ./cedana restore kata test_vm_2 -d dmp.tar
    ```

    The mandatory argument is the VM name of the VM into which we wish to perform the restore. In this case, it is “test_vm_2”, which is the new VM, running the dummy workload. Since the dmp.tar from the kata dump is present in the same directory as the cedana binary, we directly use “dmp.tar” as the path of the tar file.

## Checkpoint/Restore Kata CLH VMs (experimental)

This doc describes the steps that have to be performed to save-migrate-resume a kata clh vm sandbox. 

1. Install cedana on to a node with CLH
2. Use the currently installed CLH binary to create a new clh vm w/ a containerized workload
   ```bash
   kubectl apply -f - <<EOF
   apiVersion: v1
   kind: Pod
   metadata:
     name: busybox-kata-clh
   spec:
     runtimeClassName: kata-clh
     nodeSelector:
       cedana-kata: "true"
     containers:
     - name: busybox
       image: busybox
       command: ["sleep", "infinite"]
   EOF
   ```
3. Run a checkpoint
   ```bash
   cedana dump kata <sandbox-id> --vm-socket-path /run/vc/vm/<sandbox-id>/clh-api.sock --vm-snapshot --dir file:///tmp/test-ch
   ```
   and you must use the /tmp/test-ch directory, make sure it exists.
3. Replace CLH and Kata shim binaries with the cedana forked CloudHypervisor VM binary and kata shim binary.
   ```bash
   cp cloud-hypervisor /opt/kata/bin/cloud-hypervisor
   cp containerd-shim-kata-v2 /usr/local/bin/containerd-shim-kata-v2
   ```
4. Modify the containerd config.toml to add the new runtime.
   ```yaml
   [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]

        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.cedana]
          runtime_type = "io.containerd.cedana.v2"

          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.cedana.options]
            ConfigPath = "/opt/kata/share/defaults/kata-containers/configuration-clh.toml"
   ```
5. Add the new runtime class
   ```bash
    kubectl apply -f - <<EOF
    apiVersion: node.k8s.io/v1
    kind: RuntimeClass
    metadata:
      name: cedana
    handler: cedana
    EOF
   ```

6. We can now restore by simply using this new runtime class, use the previous podspec for checkpoint but replace it with the new runtime class.
   ```bash
   kubectl apply -f - <<EOF
   apiVersion: v1
   kind: Pod
   metadata:
     name: busybox-kata-clh
   spec:
     runtimeClassName: cedana
     nodeSelector:
       cedana-kata: "true"
     containers:
     - name: busybox
       image: busybox
       command: ["sleep", "infinite"]
   EOF
   ```
   and it will use the `/tmp/test-ch` directory for the restore.
