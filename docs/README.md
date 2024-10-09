# Documentation 

The cedana documentation repo hosts documentation related to running the `cedana` daemon on your machine, system architecture and the various features/components the daemon itself provides. 

For detailed documentation on our managed Kubernetes or GPU checkpointing features, please see [here](https://docs.cedana.ai). 

## Getting Started 
The simplest demonstration of checkpoint/restore can be performed on your machine. 

Start the daemon, either with the `./build-start-daemon.sh` script, or by running `sudo cedana daemon start &`. We have a couple simple scripts used for regression testing in `test/regression` that can be used. Exec one of the workloads with: 

``` sh
cedana exec -w $PWD test/regression/workload.sh
```

You can use `cedana ps` to manage actively running jobs and all checkpoints taken. 

To checkpoint the workload, pass the jobID from `cedana ps` to `cedana dump`: 

``` sh
cedana dump job cs3gkv7oruu6pi3qul4g -d /tmp
```

To restore: 

``` sh
cedana restore job cs3gkv7oruu6pi3qul4g
```

For more advanced capabilities (like runc, kata or gpu checkpoint/restore), see the how-to-guides below. For information on architecture or anything else that can get you started building out code in cedana, see the developer guides section. 

## How-to-guides 
- [Checkpoint/Restore Kata Containers (experimental)](kata/kata.md)
## Developer Guides 
- [Container support matrix](support/runtimes.md) 
