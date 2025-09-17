# Installation

For now, you can either install the daemon from source, or use the released binaries.

## Prerequisites

Since Cedana depends on [CRIU](https://criu.org), you will need to ensure it's dependencies are installed. For Ubuntu, you can install them with:

```sh
apt-get install -y python3-protobuf libnet1 libnftables1 libnl-3-200 libprotobuf-c1 iptables
```

## Build from source

To build:

```sh
make cedana
```

To install:

```sh
make install
```

To build and install (with all plugins):

```sh
make all
```

Try `make help` to see all available targets.

## Download from releases

Download the latest release from the [releases](https://github.com/cedana/cedana/releases).

```sh
curl -L -o cedana.tar.gz https://github.com/cedana/cedana/releases/download/v0.9.245/cedana-amd64.tar.gz
tar -xzvf cedana.tar.gz
chmod +x cedana
mv cedana /usr/local/bin/cedana
rm cedana.tar.gz
```

## Install CRIU

A modified version of CRIU is shipped as a plugin for Cedana, so you don't need to install it separately. You can simply do:

```sh
sudo cedana plugin install criu
```

This version of CRIU is not a requirement for Cedana, but it is recommended for certain features, such as [checkpoint/restore streamer](../guides/streamer/cr.md).

Note that, to install a plugin from the online registry, you need to be [authenticated](authentication.md). See [plugins](plugins.md) for more information.

To install CRIU independently, see the [CRIU installation guide](https://criu.org/Installation).

## Start the daemon

You can directly start the daemon with:

```sh
sudo cedana daemon start
```

The daemon requires root privileges for checkpoint/restore operations. Check the [CLI reference](../references/cli/cedana.md) for all options.

If you're a _systemd_ user, you may also install it as a service (if built from source):

```sh
make install-systemd
```

Try `make help` to see all available targets.

## Health check the daemon

The daemon can be health checked to ensure it fully supports the system and is ready to accept requests. See [health checks](health.md) for more information.
