# Running the SLURM tests locally

These are the same end-to-end SLURM tests that run in the `Test / Slurm` CI job,
but pointed at a throwaway cluster on your own machine. Running them locally is
much faster to iterate on than pushing a branch and waiting for GitHub Actions,
and you get a live cluster you can poke at when something breaks.

The tests spin up a small SLURM cluster out of Docker containers (one
controller, one compute node, one login node by default), install the locally
built Cedana binaries into it, register it with the propagator, and then run the
bats suites under `test/slurm/`.

## How it fits together

`make test-slurm` from the repo root does two things. First it creates a
`cedana-test` container (the same image CI uses), mounts your working tree and
the Docker socket into it, and copies your locally built plugins in. Then it runs
`make test-slurm` again *inside* that container, which is where bats actually
runs.

Inside the container, `test/slurm/setup_suite.bash` brings the cluster up before
any test runs. It calls `cedana-slurm/ansible/docker-deploy.sh` (over the mounted
Docker socket) to start the node containers and run the Ansible playbook against
them, installs Cedana onto the nodes, registers the cluster with the propagator,
and stages the sample jobs. Everything is torn down when the suite finishes.

Because the node containers are started through the host Docker socket, they end
up as siblings of the test container, not nested inside it. That is why the test
container needs `--privileged` and the socket mount.

## Prerequisites

- Docker running as root, with `/var/run/docker.sock` accessible. Rootless Docker
  does not work here because the cluster needs `--privileged` and `--cgroupns=host`.
- Sibling checkouts next to this repo:
  - `../cedana-slurm` (provides the Ansible playbook and the SPANK/task plugins)
  - `../cedana-samples` (provides the sample jobs the tests submit)
  The setup script auto-detects these. Override with `CEDANA_SLURM_DIR` and
  `SLURM_SAMPLES_DIR` if they live elsewhere.
- The `cedana/cedana-test:latest` image (`:cuda` for GPU runs). Pull it, or build
  it with `make docker-test` / `make docker-test-cuda`.
- A propagator URL and token. `CEDANA_URL` and `CEDANA_AUTH_TOKEN` are required;
  the suite fails fast without them.
- Storage credentials for wherever `CEDANA_CHECKPOINT_DIR` points. The default is
  `cedana://ci`, which uploads through the propagator, so the token above is
  enough. If you point it at S3/GCS you need the matching cloud credentials in
  your environment.

## Build the binaries first

The test container is populated from your host's `/usr/local/bin` and
`/usr/local/lib` (anything matching `*cedana*`, plus `criu`), so everything the
cluster needs has to be installed there before you run.

In this repo, build and install cedana and its plugins, then install CRIU
(`make all` does not pull CRIU, it comes as a downloaded plugin):

```
make all
cedana plugin install criu
```

`make all` installs `cedana` to `/usr/local/bin` and the `libcedana-*.so`
plugins (including `libcedana-slurm.so`) to `/usr/local/lib`. `cedana plugin
install criu` puts `criu` in `/usr/local/bin`.

In `../cedana-slurm`, build the SPANK/task plugins and the `cedana-slurm` binary.
The build lands under `build/<version>/`, not `/usr/local`, so copy it in:

```
cd ../cedana-slurm
make all
sudo cp build/*/cedana-slurm /usr/local/bin/
sudo cp build/*/*.so /usr/local/lib/
```

That covers `cedana-slurm`, `spank_cedana.so`, `task_cedana.so`,
`cli_filter_cedana.so`, and `job_submit_cedana.so`.

If you would rather use prebuilt artifacts instead of building, drop them in
`../artifacts` (with `cedana/cedana`, `criu/criu`, and `slurm/build/*`) and the
`make test-slurm` step copies them into the container for you.

## Run

```
export CEDANA_URL=<propagator-url>
export CEDANA_AUTH_TOKEN=<token>

make test-slurm
```

To run a subset, use bats tags. The suites are tagged `slurm`, plus `gpu`,
`preemption`, `unprivileged`, `cosched`, and so on.

```
# CPU only, skip GPU and preemption
make test-slurm TAGS='slurm,!gpu,!preemption'

# just the co-scheduling test
make test-slurm TAGS='cosched'
```

## Options

Pass these on the `make test-slurm` command line (`make test-slurm VAR=value`) or
export them before running. The SLURM-specific ones are forwarded into the test
container so the setup script inside sees them.

| Option | Default | What it does |
|--------|---------|--------------|
| `TAGS` | (all) | bats tag filter, e.g. `slurm,!gpu` |
| `GPU` | `0` | `1` uses the `:cuda` image with `--gpus=all` and enables the GPU tag |
| `PARALLELISM` | `8` | bats `--jobs`. Set `PARALLELISM=1` for SLURM; the cluster is shared state and parallel jobs interfere |
| `RETRIES` | `0` | retries per test |
| `DEBUG` | `0` | `1` keeps more logging and leaves debug output around |
| `SLURM_BASE_IMAGE` | (unset) | use a prebaked node image instead of building SLURM from source |
| `COMPUTE_NODES` | `1` | number of compute node containers |
| `LOGIN_NODES` | `1` | number of login node containers |
| `NFS_ROOT_SQUASH` | `1` | `0` exports the shared dirs with `no_root_squash` |

The default `PARALLELISM` is 8, which is wrong for SLURM since the whole suite
shares one cluster. Always pass `PARALLELISM=1` here:

```
make test-slurm PARALLELISM=1
```

`GPU=1` selects the CUDA image and turns on the `gpu` tag, but note the GPU
sample tests in `test/slurm/gpu.bats` are currently skipped (GPU checkpoints
aren't registered by the propagator yet), so a `GPU=1` run mainly exercises the
CUDA build of the non-GPU paths.

## Prebaked images vs building from source

By default the cluster compiles SLURM from source inside the controller during
setup. That is the slow part, roughly ten to fifteen minutes.

To skip it, point `SLURM_BASE_IMAGE` at a prebaked node image. These are
published as `cedana/cedana-slurm-node:slurm-<version>` and ship SLURM already
built, so setup drops to a minute or two.

```
export SLURM_BASE_IMAGE=cedana/cedana-slurm-node:slurm-25-11-5-1
make test-slurm
```

The image's SLURM version must match the `cedana-slurm` plugins you built or
installed, otherwise the plugin fails to load against a different `libslurm`
soname. `docker-deploy.sh` validates the baked `slurmctld` version against the
version the deploy expects and aborts on a mismatch.

You can build a node image yourself from `../cedana-slurm`:

```
cd ../cedana-slurm
docker build -t cedana-slurm-node:slurm-25-11-5-1 \
  --build-arg SLURM_TAG=slurm-25-11-5-1 \
  -f docker/slurm-node.Dockerfile .
```

## Debugging a failing run

Get a shell in the test container without running the suite. Use
`test-enter-slurm`, not `test-enter`: only the SLURM variant mounts
`../cedana-slurm` at `/cedana-slurm` and copies the plugins in.

```
make test-enter-slurm            # add GPU=1 for the CUDA image
```

From there you can bring the cluster up by hand and inspect it:

```
cd /cedana-slurm/ansible
COMPUTE_NODES=1 LOGIN_NODES=1 bash docker-deploy.sh

docker exec slurm-controller sinfo
docker exec slurm-controller squeue
docker exec slurm-controller scontrol show config | grep -i plugindir
docker exec slurm-compute-01 tail -f /var/log/slurm/slurmd.log
docker exec slurm-controller tail -f /var/log/slurm/slurmctld.log
```

The node containers are named `slurm-controller`, `slurm-compute-01`, and
`slurm-login-01`. Munge, slurmctld, and slurmd run under systemd inside them, so
`journalctl -u slurmctld` works too.

Run a single suite directly instead of the whole set:

```
bats test/slurm/cpu.bats
bats test/slurm/preemption.bats
```

## Cleanup

A normal run tears the cluster down on its own. If a run is interrupted, the node
containers and network can be left behind. This removes the controller plus any
numbered compute and login nodes (adjust if you ran with more), the network, and
the outer test container:

```
docker rm -f $(docker ps -aq --filter 'name=^slurm-')
docker network rm slurm-net
docker rm -f cedana-test
```

## Troubleshooting

`CEDANA_URL is not set` or `CEDANA_AUTH_TOKEN is not set`: export both before
running. The suite checks for them before doing anything.

`Cannot connect to the Docker daemon`: the test container talks to the host
daemon over the mounted socket. Make sure Docker is running as root and the
socket path is correct.

Node containers time out during setup: check the controller boots cleanly with
`docker logs slurm-controller`. A single failed systemd unit can leave the node
in a `degraded` state; the readiness wait tolerates `degraded` but a hard failure
(munge without a key, for example) will stall it.

`dlopen(...): libslurm.so.NN: cannot open shared object file`: the SLURM version
in the node image does not match the `cedana-slurm` plugin you installed. Rebuild
the plugin against the same version, or use a matching `SLURM_BASE_IMAGE`.

A test user's job stays `PENDING (Resources)` forever: the compute node cannot
fit a second job. This usually means the node is advertising a single core; the
cluster config advertises the runner's CPUs as individual cores so multiple
users' jobs can co-schedule, so check `scontrol show node` if you have changed
the node topology.
