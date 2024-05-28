import json
import matplotlib.pyplot as plt
import numpy as np
import random

# n users work 8 hours a day from 9am until 5pm
# time (min) | utilization | suspend | resume
# -----------|-------------|---------|--------
# 0 - 539    | 0           | 0       | 0
# 540 (9am)  | 1           | 0       | 1
# 541 - 1019 | 1           | 0       | 0
# 1020 (5pm) | 0           | 1       | 0
# for each minute, make a json object e.g.
# {
#   "time": 0,
#   "utilization": 0,
#   "suspend": 0,
#   "resume": 0,
# }
# for each user, make a json object e.g.
# {
#   "id": 0,
#   "usage data": {prev json object}
# }
def naive(n: int):
    usage = {}
    for id in range(n):
        usage_data = []
        for t in range(1440): # minute
            u = 1 if t > 539 and t < 1020 else 0
            s = 1 if t == 1020 else 0
            r = 1 if t == 540 else 0
            usage_data.append({
                "time": t,
                "utilization": u,
                "suspend": s,
                "resume": r,
            })
        usage["user " + str(id)] = {
            "id": id,
            "usage_data": usage_data
        }
    return json.dumps(usage)

# n users work 8 hours a day at times that vary based on start_range
def monte_carlo_8_hr(n: int, start_range=(360,720)): # 6am-12pm
    usage = {}
    for id in range(n):
        usage_data = []
        start_time = random.randint(start_range[0], start_range[1])
        end_time = start_time + 8*60
        for t in range(1440): # minute
            u = 1 if t >= start_time and t < end_time else 0
            s = 1 if t == end_time else 0
            r = 1 if t == start_time else 0
            usage_data.append({
                "time": t,
                "utilization": u,
                "suspend": s,
                "resume": r,
            })
        usage["user " + str(id)] = {
            "id": id,
            "usage_data": usage_data
        }
    return json.dumps(usage)

# n users work any hours a day at times that vary based on start_range and end_range
def monte_carlo_any_hr(n: int, start_range=(360, 720), end_range=(840, 1200)):
    usage = {}
    for id in range(n):
        usage_data = []
        start_time = random.randint(start_range[0], start_range[1])
        end_time = random.randint(end_range[0], end_range[1])
        for t in range(1440): # minute
            u = 1 if t >= start_time and t < end_time else 0
            s = 1 if t == end_time else 0
            r = 1 if t == start_time else 0
            usage_data.append({
                "time": t,
                "utilization": u,
                "suspend": s,
                "resume": r,
            })
        usage["user " + str(id)] = {
            "id": id,
            "usage_data": usage_data
        }
    return json.dumps(usage)

def plot_utilization(usage_json, filename='utilization_plot.png'):
    usage = json.loads(usage_json)

    # init plot
    plt.figure(figsize=(15, 10))

    # define line styles and markers
    line_styles = ['-', '--', '-.', ':']
    markers = ['o', 's', '^', 'D', 'v', '>', '<', 'p', 'h', '*']

    for idx, (user_key, user_data) in enumerate(usage.items()):
        id = user_data['id']
        usage_data = user_data['usage_data']

        times = [entry['time'] for entry in usage_data]
        utilizations = [entry['utilization'] for entry in usage_data]

        # to differentiate users more clearly
        line_style = line_styles[idx % len(line_styles)]
        marker = markers[idx % len(markers)]

        plt.plot(times, utilizations, label=f'User {id}', linestyle=line_style, marker=marker, alpha=0.7)

    # display time in hours
    plt.xticks(np.arange(0, 1440, 60), [f'{h:02}:00' for h in range(24)], rotation=45)
    
    # display discrete utilization
    plt.yticks([0, 1], ['0', '1'])
    
    # add labels
    plt.xlabel('Time')
    plt.ylabel('Utilization')
    plt.title('User Utilization Over Time')
    plt.legend(loc='upper right')

    # save to file
    plt.savefig(filename)
    plt.close()

def main():
    plot_utilization(naive(10), "naive.png")
    plot_utilization(monte_carlo_8_hr(10), "8hr.png")
    plot_utilization(monte_carlo_any_hr(10), "normal_range.png")
    plot_utilization(monte_carlo_any_hr(10,(0,1440),(0,1440)), "any.png")

if __name__ == "__main__":
    main()