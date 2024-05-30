import cedana_bindings as cedana
import gen_usage
import json
from tplib import task_pb2

async def main(daemonPID, remote):
    print("continuous")
    base_8 = json.loads(gen_usage.monte_carlo_len_spec(10, (540,540)))
    for user in base_8:
        jobID = "vscode-continuous" + str(base_8[user]['id'])
        started = False
        process_stats = {}
        ckptID = ""
        usage_data = base_8[user]['usage_data']
        for usage_slice in usage_data:
            print(usage_slice)
            if usage_slice['resume']:
                if not started:
                    process_stats = await cedana.run_exec("code-server --bind-addr localhost:1234", jobID)
                    started = True
                else:
                    await cedana.run_restore(daemonPID, jobID, ckptID, cedana.output_dir, remote)
            elif usage_slice['suspend']:
                dump_resp = await cedana.run_checkpoint(daemonPID, jobID, cedana.output_dir, process_stats, remote)
                ckptID = dump_resp.CheckpointID
            # TODO migrate
