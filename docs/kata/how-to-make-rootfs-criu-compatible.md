Cedana has a fork of the [kata-containers](https://github.com/cedana/kata-containers) repository. Certain files have been changed for enabling the creation of an Ubuntu RootFS. The changes are configurational in nature, and can be found in [this](https://github.com/cedana/kata-containers/commit/946b2637c1491d47dfec0e772f73c496e78490a1) commit. Docker should be installed on the host machine.

```bash
cd kata-containers/tools/osbuilder/rootfs-builder
sudo USE_DOCKER=true ./rootfs.sh ubuntu
```

This creates a folder, “rootfs-ubuntu” which will be attached to the guest VM. We will now move the CRIU source code into the rootfs

```bash
cd kata-containers/tools/osbuilder/rootfs-builder/rootfs-ubuntu/
git clone https://github.com/checkpoint-restore/criu.git
```

Additionally, we also need to move the [CRIU builder + daemon launcher script](../../scripts/kata-utils/build_start_daemon.sh) for the guest into the rootfs. The location in the rootfs is `kata-containers/tools/osbuilder/rootfs-builder/rootfs-ubuntu/bin`

Now that we have a rootfs, we need to create an image out of it. This step is simple. 

```bash
cd kata-containers/tools/osbuilder/image-builder
sudo USE_DOCKER=true ./image_builder.sh ../rootfs-builder/rootfs-ubuntu/
```

The final argument is the location of the rootfs directory created in the previous step. This creates an image file called “kata-containers.img”. We need to copy the img into the appropriate place as per the config file 

```bash
cd kata-containers/tools/osbuilder/image-builder
sudo cp kata-containers.img /usr/share/kata-containers/
```