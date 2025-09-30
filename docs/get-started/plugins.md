# Plugins

Plugins extend the support of Cedana's checkpoint/restore to various container runtimes, GPUs, etc. Plugins can either be installed from the online registry or built locally.

## Online registry

{% hint style="success" %}
To access released plugins, you need to be authenticated. See [authentication](authentication.md).
{% endhint %}

You can list all available plugins with:

```sh
cedana plugin list
```

```
PLUGIN      SIZE    STATUS     INSTALLED VERSION    AVAILABLE VERSION  PUBLISHED
criu        6 MiB   outdated   v3.9                 v4.0               one month ago
runc        35 MiB  installed  v0.9.239             v0.9.239           2 hours ago
containerd  38 MiB  installed  v0.9.239             v0.9.239           2 hours ago
crio        27 MiB  installed  v0.9.239             v0.9.239           2 hours ago
k8s         32 MiB  installed  v0.9.239             v0.9.239           2 hours ago
gpu         32 MiB  available                       v0.4.7             2 minutes ago
streamer    1 MiB   available                       v0.0.6             9 days ago

7 installed, 0 available
```

This will show you all the plugins that are available, installed, or outdated.

You can also list specific plugins by name & version:

```sh
cedana plugin list gpu@v0.5.0 streamer@v0.0.5
```

```sh
PLUGIN    SIZE    STATUS     INSTALLED VERSION  AVAILABLE VERSION  PUBLISHED
gpu       48 MiB  available                     v0.5.0             one day ago
streamer  6 MiB   available                     v0.0.5             4 months ago

0 installed, 2 available
```

## Locally built plugins

If plugins are built locally (in the current directory), running `cedana plugin list` in the current directory will show the locally built plugins instead:

```
PLUGIN      SIZE    STATUS     INSTALLED VERSION    AVAILABLE VERSION  PUBLISHED
criu        6 MiB   outdated   v3.9                 v4.0               one month ago
runc        35 MiB  installed  v0.9.239             local              2 hours ago
containerd  38 MiB  installed  v0.9.239             local              2 hours ago
crio        27 MiB  installed  v0.9.239             local              2 hours ago
k8s         32 MiB  installed  v0.9.239             local              2 hours ago
gpu         32 MiB  available                       v0.4.7             2 minutes ago
streamer    1 MiB   available                       v0.0.6             9 days ago

7 installed, 0 available
```

Notice the `LATEST VERSION` column shows `local` for locally built plugins.

Instead of depending on current directory, you can also specify the paths to search for locally built plugins by setting the `CEDANA_PLUGINS_LOCAL_SEARCH_PATH` (paths are colon-separated just like the `PATH` env var). This convenient for developers who are working on multiple plugins at once.

{% hint style="info" %}
Check out the guide on [writing plugins](../developer-guides/writing_plugins.md) if you're interested in building your own plugins.
{% endhint %}

## Install

Once a plugin appears in the list, you can install it with:

```sh
sudo cedana plugin install <plugin>@<version> <plugin> ...
```

## Uninstall

To uninstall a plugin, use:

```sh
sudo cedana plugin remove <plugin> <plugin> ...
```

{% hint style="info" %}
Check out the [CLI reference](../references/cli/cedana_plugin.md) for all plugin-related subcommands.
{% endhint %}

## Features

To see which features are supported by the installed plugins, use:

```sh
cedana plugin features
```

{% hint style="info" %}
See [features](features.md) for more information.
{% endhint %}

## Health check

The full health check command will also check the health of all installed plugins. See [health checks](health.md).
