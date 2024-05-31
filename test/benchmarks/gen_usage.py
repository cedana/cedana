import json
import matplotlib.pyplot as plt
import numpy as np
import random

# n users work hrs_worked hours a day based on specified start_range
def monte_carlo_len_spec(n: int, start_range=(360,720), hrs_worked=8):
    usage = {}
    for id in range(n):
        usage_data = []
        start_time = random.randint(start_range[0], start_range[1])
        end_time = start_time + hrs_worked*60
        for t in range(1440): # minute
            u = 1 if t >= start_time and t < end_time else 0
            s = 1 if t == end_time else 0
            m = 1 if t == start_time else 0
            r = 1 if t == start_time + 1 else 0
            usage_data.append({
                "time": t,
                "utilization": u,
                "suspend": s,
                "migrate": m,
                "resume": r,
            })
        usage["user " + str(id)] = {
            "id": id,
            "usage_data": usage_data
        }
    return usage

# n users work at times based on specified start_range and end_range
def monte_carlo_range_spec(n: int, start_range=(360, 720), end_range=(840, 1200)):
    usage = {}
    for id in range(n):
        usage_data = []
        a = random.randint(start_range[0], start_range[1])
        b = random.randint(end_range[0], end_range[1])
        while abs(a-b) < 15: # work minimum 15 minutes
            a = random.randint(start_range[0], start_range[1])
            b = random.randint(end_range[0], end_range[1])
        start_time = min(a, b)
        end_time = max(a, b)
        for t in range(1440): # minute
            u = 1 if t >= start_time and t < end_time else 0
            s = 1 if t == end_time else 0
            m = 1 if t == start_time else 0
            r = 1 if t == start_time + 1 else 0
            usage_data.append({
                "time": t,
                "utilization": u,
                "suspend": s,
                "migrate": m,
                "resume": r,
            })
        usage["user " + str(id)] = {
            "id": id,
            "usage_data": usage_data
        }
    return usage

def plot_utilization(usage, filename='utilization_plot.png'):
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

def save_json_to_file(usage, filename):
    with open(filename, "w") as json_file:
        json_file.write(json.dumps(usage))

def main():
    # s = start, e = end
    # time in 24h format
    base_8 = monte_carlo_len_spec(10, (540,540)) # s = 9, e = 17
    range_8 = monte_carlo_len_spec(10) # s = rand(6-12), e = s + 8h
    base_r = monte_carlo_range_spec(10) # s = rand(6-12), e = rand(14-20)
    any_r = monte_carlo_range_spec(10, (0,1440), (0,1440)) # s, e = rand(0-23:59) | e >= s + 15m
    plot_utilization(base_8, "naive.png")
    plot_utilization(range_8, "8hr.png")
    plot_utilization(base_r, "normal_range.png")
    plot_utilization(any_r, "any_range.png")
    save_json_to_file(base_8, "base_8.json")
    save_json_to_file(base_r, "base_r.json")

if __name__ == "__main__":
    main()
