import json

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


def main():
    print(naive(5))

if __name__ == "__main__":
    main()