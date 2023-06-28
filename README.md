# Cedana

Build systems that bake realtime adaptiveness and elasticity using Cedana.

Cedana-client serves as client code to the larger Cedana system. We leverages CRIU to provide checkpoint and restore functionality for most linux processes (including docker containers).

We can monitor, migrate and automate checkpoints across a realtime network and compute configuration enabling ephemeral and hardware agnostic compute. See [our website]cedana.ai for more information about our managed product. 

Some problems Cedana can help solve include: 
- Cold-starts for containers/processes 
- Keeping a process running independent of hardware/network failure 
- Managing multiprocess/multinode systems 

## Architecture 
TODO

## Build

```go build```

## Usage
To use Cedana in a standalone context, you can directly checkpoint and restore processes with: 

```sh 
cedana client dump/restore -p PID
```
