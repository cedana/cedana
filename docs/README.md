# Documentation 
> [!NOTE]
> This documentation is a work in progress.

The cedana documentation repo hosts documentation related to running the `cedana` daemon on your machine, system architecture and the various features/components the daemon itself provides. 

For detailed documentation on our managed Kubernetes or the larger cedana system, please see [here](https://docs.cedana.ai). 

## Getting Started 
The simplest demonstration of checkpoint/restore can be performed on your machine. 

Start the daemon by running `make start`. Try out a simple scripts in `test/workloads`:

``` sh
cedana run process test/workloads/date-loop.sh
```

You can use `cedana ps` to manage actively running jobs and all checkpoints taken. 
``` sh
$ cedana ps
JOB             TYPE         PID  STATUS  GPU  CHECKPOINT  SIZE  LOG
angry_hypatia9  process  3489970  sleep   no                     /var/log/cedana-output-angry_hypatia9.log
```

To checkpoint the workload, pass the job name from `cedana ps` to `cedana dump`:

``` sh
cedana dump job angry_hypatia9
```

To restore: 

``` sh
cedana restore job angry_hypatia9
```

For more advanced capabilities (like runc, kata or gpu checkpoint/restore), see the how-to-guides below. For information on architecture or anything else that can get you started building out code in cedana, see the developer guides section. 

## How-to-guides 
- [Checkpoint/Restore kata containers](kata/kata.md)
- [Checkpoint/Restore GPU runc containers](runc/gpu.md)
- [Checkpoint/Restore with cedana-image-streamer](cedana-image-streamer/cedana-image-streamer.md)

## Developer Guides 
- [Container runtime support](support/runtimes.md) 
