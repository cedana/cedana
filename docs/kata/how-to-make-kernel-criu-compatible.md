This doc describes how we can build a kernel which is CRIU compatible. `Go` must be installed. Other package requirements can be installed as follows :

```bash
go install github.com/mikefarah/yq/v4@latest
sudo apt install flex
sudo apt install bison
sudo apt install libelf-dev
```

The vanilla build steps for 5.15.26 version of the kernel are : 

```bash
sudo ./build-kernel.sh -v 5.15.26 setup
sudo ./build-kernel.sh -v 5.15.26 build
sudo ./build-kernel.sh -v 5.15.26 install
```

Since CRIU has optimizations for kernel versions post 6.x.x, we will build 6.1.62, using the configuration file directly. The configuration file contains all the flags set (as required for CRIU, mentioned [here](https://criu.org/Linux_kernel)). The configuration file is added to the Cedana fork of kata-containers, in [this](https://github.com/cedana/kata-containers/commit/34183f9b4ad0eeebffd95dc6518571b7c3ae8bd0) commit. 

```bash
sudo ./build-kernel.sh -v 6.1.62 -c /home/ubuntu/kata-containers/tools/packaging/kernel/configs/amd64-6.1.62-criu-compatible.conf setup
```

The command above will ask certain config details, you can answer them as follows :

```bash
Track memory changes (MEM_SOFT_DIRTY) [N/y/?] (NEW) y
UDP: socket monitoring interface (INET_UDP_DIAG) [N/y/?] (NEW) y
RAW: socket monitoring interface (INET_RAW_DIAG) [N/y/?] (NEW) y
INET: allow privileged process to administratively close sockets (INET_DIAG_DESTROY) [N/y/?] (NEW) N
```

The build and install steps can continue as before : 

```bash
sudo ./build-kernel.sh -v 6.1.62 build
sudo ./build-kernel.sh -v 6.1.62 install
```

This will finally create “/usr/share/kata-containers/vmlinux.container”. We would need to edit the config file to change the kernel.

```bash
Path : /opt/kata/share/defaults/kata-containers/configuration-qemu.toml
kernel = "/usr/share/kata-containers/vmlinux.container"
```

We can now check the config by logging inside the guest VM. Example : 

```bash
zcat /proc/config.gz | grep CONFIG_CHECKPOINT_RESTORE
```