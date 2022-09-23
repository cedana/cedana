# Cedana

## Fast and efficient checkpointing client for real-time and distributed systems

cedana leverages CRIU to provide checkpoint and restore functionality for most linux processes. With the addition of an orchestrator (leveraging the gRPC definitions), we can monitor and migrate checkpoints across a predefined network and compute configuration enabling ephemeral and hardware agnostic (depending on architecture) compute.

## Architecture 
TODO

## Build

```go build```

## Usage

At it's most basic level, `cedana` functions as an extension to [criu](https://criu.org/Main_Page) and leverages [go-criu](https://github.com/checkpoint-restore/go-criu) to do so.

To checkpoint a running process:

```./cedana client dump -p PROCESS -d DIR```

To restore the same process:

```./cedana client restore -d DIR```

The added functionality offered by the `cedana` cli is to make it easier to add hooks to pre and post dump/restores. You can write bash scripts, stick them in the `scripts` folder, and modify `client_config` accordingly. 

Checkpointing and restoring docker containers is still an experimental feature, but you can go about it with similar syntax: 

```./cedana client docker dump -c CONTAINER_NAME -d DIR``` 

Leaving the container running or not is toggle-able via config. To restore from the latest checkpoint: 

```./cedana client docker restore -c CONTAINER_NAME```

### Demo
[code-server](https://github.com/coder/code-server) is checkpointed, killed and restored, demonstrating restoration of a TCP connection. 
![demo](https://user-images.githubusercontent.com/409327/190646592-6a2db9b0-d0c8-4e3b-9511-f7fa2245e393.gif)



## Note
This is still a WIP! There's a lot to be done, so use with caution.

## References
