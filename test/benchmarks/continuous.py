import cedana_bindings as cedana
from datetime import datetime
import gen_usage
import json
import time
from tplib import task_pb2

def get_24h_usage_data(name):
    if name == "base":
        return gen_usage.base()
    elif name == "9to5":
        return gen_usage.monte_carlo_len_spec((540,540))
    elif name == "8h_varied":
        return gen_usage.monte_carlo_len_spec()
    elif name == "std_range":
        return gen_usage.monte_carlo_range_spec()
    elif name == "any_range":
        return gen_usage.monte_carlo_range_spec((0,1440))
    elif name == "on_off":
        return gen_usage.on_and_off(5)
    else:
        print("Unknown name `" + name + "` for get_24h_usage_data")

def get_current_time():
    t = datetime.now()
    return t.hour, t.minute, t.second

async def run_continuous(daemonPID, remote, name):
    jobID = "vscode-continuous-" + name

    # initial exec
    usage = get_24h_usage_data(name)
    process_stats = await cedana.run_exec("code-server --bind-addr localhost:1234", jobID)
    ckptID = ""
    h, m, s = get_current_time()
    print("{:02d}:{:02d}:{:02d} => {} minutes".format(h, m, s, h * 60 + m))
    usage_slice = usage.iloc[h * 60 + m]
    print(dict(zip(usage.columns.tolist(),usage_slice.values)))
    if usage_slice['suspend']:
        dump_resp = await cedana.run_checkpoint(daemonPID, jobID, cedana.output_dir, process_stats, remote)
        ckptID = dump_resp.CheckpointID
    # sleep until beginning of next minute
    time.sleep(60 - datetime.now().second)

    # continuous simulation
    while True:
        h, m, s = get_current_time()
        print("{:02d}:{:02d}:{:02d} => {} minutes".format(h, m, s, h * 60 + m))
        usage_slice = usage.iloc[h * 60 + m]
        print(dict(zip(usage.columns.tolist(),usage_slice.values)))
        if usage_slice['suspend']:
            dump_resp = await cedana.run_checkpoint(daemonPID, jobID, cedana.output_dir, process_stats, remote)
            ckptID = dump_resp.CheckpointID
        # elif usage_slice['migrate']: # TODO migrate
        elif usage_slice['restore']:
            await cedana.run_restore(daemonPID, jobID, ckptID, cedana.output_dir, remote)

        # get new usage data every 24h
        if h == 23 and m == 59:
            usage = get_24h_usage_data(name)

        time.sleep(60 - datetime.now().second)

async def main(daemonPID, remote, name):
    await run_continuous(daemonPID, remote, name)
