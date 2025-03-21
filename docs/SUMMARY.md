# Table of contents

* [Cedana Daemon](README.md)

## Get started

* [Installation](get-started/installation.md)
* [Authentication](get-started/authentication.md)
* [Configuration](get-started/configuration.md)
* [Health checks](get-started/health.md)
* [Plugins](get-started/plugins.md)
* [Feature matrix](get-started/features.md)

## Guides

* [Managed process/container](guides/managed.md)
* [Checkpoint/restore basics](guides/cr.md)
* [Checkpoint/restore with GPUs](guides/gpu/cr.md)
* [Checkpoint/restore runc](guides/runc/cr.md)
* [Checkpoint/restore containerd](guides/containerd/cr.md)
* [Checkpoint/restore streamer](guides/streamer/cr.md)
* [Checkpoint/restore kata](guides/kata/README.md)
  * [how-to-create-custom-busybox-image](guides/kata/how-to-create-custom-busybox-image.md)
  * [how-to-install-criu-in-guest](guides/kata/how-to-install-criu-in-guest.md)
  * [how-to-install-on-aws](guides/kata/how-to-install-on-aws.md)
  * [how-to-make-kernel-criu-compatible](guides/kata/how-to-make-kernel-criu-compatible.md)
  * [how-to-make-rootfs-criu-compatible](guides/kata/how-to-make-rootfs-criu-compatible.md)
  * [Checkpoint/Restore kata containers](guides/kata/kata.md)

## Developer guides

* [Architecture](developer-guides/architecture.md)
* [Profiling](developer-guides/profiling.md)
* [Testing](developer-guides/testing.md)
* [Writing plugins](developer-guides/writing_plugins.md)

## References

* [CLI](references/cli/README.md)
  * [cedana](references/cli/cedana.md)
  * [cedana attach](references/cli/cedana_attach.md)
  * [cedana checkpoint](references/cli/cedana_checkpoint.md)
  * [cedana checkpoints](references/cli/cedana_checkpoints.md)
  * [cedana completion](references/cli/cedana_completion.md)
  * [cedana completion bash](references/cli/cedana_completion_bash.md)
  * [cedana completion fish](references/cli/cedana_completion_fish.md)
  * [cedana completion powershell](references/cli/cedana_completion_powershell.md)
  * [cedana completion zsh](references/cli/cedana_completion_zsh.md)
  * [cedana daemon](references/cli/cedana_daemon.md)
  * [cedana daemon check](references/cli/cedana_daemon_check.md)
  * [cedana daemon start](references/cli/cedana_daemon_start.md)
  * [cedana delete](references/cli/cedana_delete.md)
  * [cedana dump](references/cli/cedana_dump.md)
  * [cedana dump containerd](references/cli/cedana_dump_containerd.md)
  * [cedana dump job](references/cli/cedana_dump_job.md)
  * [cedana dump process](references/cli/cedana_dump_process.md)
  * [cedana dump runc](references/cli/cedana_dump_runc.md)
  * [cedana exec](references/cli/cedana_exec.md)
  * [cedana features](references/cli/cedana_features.md)
  * [cedana inspect](references/cli/cedana_inspect.md)
  * [cedana job](references/cli/cedana_job.md)
  * [cedana job attach](references/cli/cedana_job_attach.md)
  * [cedana job checkpoint](references/cli/cedana_job_checkpoint.md)
  * [cedana job checkpoint inspect](references/cli/cedana_job_checkpoint_inspect.md)
  * [cedana job checkpoint list](references/cli/cedana_job_checkpoint_list.md)
  * [cedana job checkpoints](references/cli/cedana_job_checkpoints.md)
  * [cedana job delete](references/cli/cedana_job_delete.md)
  * [cedana job inspect](references/cli/cedana_job_inspect.md)
  * [cedana job kill](references/cli/cedana_job_kill.md)
  * [cedana job list](references/cli/cedana_job_list.md)
  * [cedana jobs](references/cli/cedana_jobs.md)
  * [cedana k8s-helper](references/cli/cedana_k8s-helper.md)
  * [cedana k8s-helper destroy](references/cli/cedana_k8s-helper_destroy.md)
  * [cedana kill](references/cli/cedana_kill.md)
  * [cedana manage](references/cli/cedana_manage.md)
  * [cedana manage containerd](references/cli/cedana_manage_containerd.md)
  * [cedana manage process](references/cli/cedana_manage_process.md)
  * [cedana manage runc](references/cli/cedana_manage_runc.md)
  * [cedana plugin](references/cli/cedana_plugin.md)
  * [cedana plugin features](references/cli/cedana_plugin_features.md)
  * [cedana plugin install](references/cli/cedana_plugin_install.md)
  * [cedana plugin list](references/cli/cedana_plugin_list.md)
  * [cedana plugin remove](references/cli/cedana_plugin_remove.md)
  * [cedana plugins](references/cli/cedana_plugins.md)
  * [cedana ps](references/cli/cedana_ps.md)
  * [cedana query](references/cli/cedana_query.md)
  * [cedana query k8s](references/cli/cedana_query_k8s.md)
  * [cedana query runc](references/cli/cedana_query_runc.md)
  * [cedana restore](references/cli/cedana_restore.md)
  * [cedana restore job](references/cli/cedana_restore_job.md)
  * [cedana restore process](references/cli/cedana_restore_process.md)
  * [cedana restore runc](references/cli/cedana_restore_runc.md)
  * [cedana run](references/cli/cedana_run.md)
  * [cedana run containerd](references/cli/cedana_run_containerd.md)
  * [cedana run process](references/cli/cedana_run_process.md)
  * [cedana run runc](references/cli/cedana_run_runc.md)
* [API](references/api.md)
* [GitHub](https://github.com/cedana/cedana)
