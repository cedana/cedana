# Cedana

## Fast and efficient checkpointing client for real-time and distributed systems

Cedana-client leverages CRIU to provide checkpoint and restore functionality for most linux processes. With the addition of an orchestrator (leveraging the gRPC definitions), we can monitor and migrate checkpoints across a predefined network and compute configuration enabling ephemeral and potentially hardware agnostic compute.

## Architecture 
TODO

## Build

```go build```

## Usage

At it's most basic level, `cedana-client` functions as an extension to [criu](https://criu.org/Main_Page) and leverages [go-criu](https://github.com/checkpoint-restore/go-criu) to do so.

To checkpoint a running process:

```./cedana-client client dump -p PROCESS -d DIR```

To restore the same process:

```./cedana-client client restore -d DIR```

The added functionality offered by the `cedana` cli is to make it easier to add hooks to pre and post dump/restores. You can write bash scripts, stick them in the `scripts` folder, and modify `client_config` accordingly.

### Demo
[code-server](https://github.com/coder/code-server) is checkpointed, killed and restored, demonstrating restoration of a TCP connection. 

[demo](https://www.youtube.com/watch?v=1MVj7rJemDM)

## Note
This is still a WIP! There's a lot to be done, so use with caution. We are in the process of taking out code from our orchestrator platform and adding it to this repo, so there's a lot more to be added.

## References
