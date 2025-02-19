# how-to-install-criu-in-guest

Since the `img` option attached the filesystem in a RO manner, we use the `/tmp` directory which is RW in nature. The reason for this is that /tmp is attached using tmpfs (RAM backed temporary storage). CRIU source code needs to be present inside the rootfs.

```bash
cp -r criu/ tmp/
cd tmp/criu
make
export PATH=$PATH:/tmp/criu/criu
```

These steps are already present in the [CRIU builder + cedana daemon runner script](../../../scripts/kata-utils/build_start_daemon.sh)
