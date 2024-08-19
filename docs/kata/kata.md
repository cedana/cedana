This doc describes the steps that have to be performed to save-migrate-resume a kata-container workload. It includes building a CRIU compatible kernel and rootfs, and a busy-box image which includes the cedana wrapper. These steps are also exactly what are performed by an end-to-end test for kata container workloads which are checkpointed and restored using cedana. 

1. Get the fork of kata-containers, created under cedana. Switch to the rootfs-kernel branch. 
2. Create the rootfs : 
    
    1. 
        ```bash
        cd kata-containers/tools/osbuilder/rootfs-builder
        sudo USE_DOCKER=true ./rootfs.sh ubuntu 
        ```
    2. Add the CRIU source folder and the compiled cedana binary into the `kata-containers/tools/osbuilder/rootfs-builder/rootfs-ubuntu` directory. 
    3. Add the CRIU installation + cedana runner script (`build_start_daemon.sh`) into the `kata-containers/tools/osbuilder/rootfs-builder/rootfs-ubuntu/bin` directory. It is critical this script is placed in the bin, so that it is runnable from anywhere when we log into the guest. 
3. Create the VM image: 

```bash
cd kata-containers/tools/osbuilder/image-builder
sudo USE_DOCKER=true ./image_builder.sh ../rootfs-builder/rootfs-ubuntu/
```

4. Create the kernel compatible with CRIU: Steps are quite dense, and listed on the main Kata [doc](https://www.notion.so/Kata-Containers-architecture-9dfdd079e16740f6a07e6c0b3af1614e?pvs=21).
5. Create a custom busybox image with the cedana wrapper: Steps are dense, and listed on the main Kata [doc](https://www.notion.so/Kata-Containers-architecture-9dfdd079e16740f6a07e6c0b3af1614e?pvs=21). 
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
