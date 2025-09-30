# Installation

For now, you can either install the daemon from source, or use the released binaries.

## Prerequisites

Since Cedana depends on [CRIU](https://criu.org), you will need to ensure it's dependencies are installed.

#### Using apt (Debian/Ubuntu)

```sh
apt-get install -y libnet-devel protobuf-c-devel libnl3-devel libbsd-devel libcap-devel libseccomp-devel gpgme-devel nftables-devel
```

#### Using dnf/yum (Fedora/CentOS)

```sh
yum install -y libnet-dev libprotobuf-c-dev libnl-3-dev libbsd-dev libcap-dev libseccomp-dev libgpgme11-dev libnftables1
```

## Build from source

#### Build

```sh
make cedana
```

#### Install

```sh
make install
```

#### Build and install (with all plugins)

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

{% hint style="success" %}
To install a plugin from the online registry, you need to be [authenticated](authentication.md). See [plugins](plugins.md) for more information.
{% endhint %}

A modified version of CRIU is shipped as a plugin for Cedana, so you don't need to install it separately. You can simply do:

```sh
sudo cedana plugin install criu
```

This version of CRIU is not a requirement for Cedana, but it is recommended for certain features, such as [checkpoint/restore streamer](../guides/streamer/cr.md).

{% hint style="info" %}
To install CRIU independently, see the [CRIU installation guide](https://criu.org/Installation).
{% endhint %}

## Start the daemon

{% hint style="warning" %}
The daemon requires root privileges for checkpoint/restore operations. Check the [CLI reference](../references/cli/cedana.md) for all options.
{% endhint %}

#### Direct

You can directly start the daemon with:

```sh
sudo cedana daemon start
```

#### Systemd

If you're a _systemd_ user, you may also install it as a service (if built from source):

```sh
make install-systemd
```

{% hint style="info" %}
Try `make help` to see all available targets.
{% endhint %}

## Health check the daemon

The daemon can be health checked to ensure it fully supports the system and is ready to accept requests. See [health checks](health.md) for more information.
