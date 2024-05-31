import json
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd
import random

# n users work hrs_worked hours a day based on specified start_range
def monte_carlo_len_spec(start_range=(360,720), hrs_worked=8):
    s = random.randint(start_range[0], start_range[1])
    e = s + hrs_worked*60

    usage = pd.DataFrame({'time': range(1440)})
    usage['utilization'] = usage['time'].apply(lambda t: 1 if t >= s and t < e else 0)
    usage['suspend'] = usage['time'].apply(lambda t: 1 if t == e else 0)
    usage['migrate'] = usage['time'].apply(lambda t: 1 if t == s else 0)
    usage['resume'] = usage['time'].apply(lambda t: 1 if t == s + 1 else 0)

    return usage

# n users work at times based on specified start_range and end_range
def monte_carlo_range_spec(start_range=(360, 720), end_range=(840, 1200)):
    a = random.randint(start_range[0], start_range[1])
    b = random.randint(end_range[0], end_range[1])
    while abs(a-b) < 15: # work minimum 15 minutes
        a = random.randint(start_range[0], start_range[1])
        b = random.randint(end_range[0], end_range[1])
    s = min(a, b)
    e = max(a, b)

    usage = pd.DataFrame({'time': range(1440)})
    usage['utilization'] = usage['time'].apply(lambda t: 1 if t >= s and t < e else 0)
    usage['suspend'] = usage['time'].apply(lambda t: 1 if t == e else 0)
    usage['migrate'] = usage['time'].apply(lambda t: 1 if t == s else 0)
    usage['resume'] = usage['time'].apply(lambda t: 1 if t == s + 1 else 0)

    return usage

def main():
    # s = start, e = end
    # time in 24h format
    base_8 = monte_carlo_len_spec((540,540)) # s = 9, e = 17
    range_8 = monte_carlo_len_spec() # s = rand(6-12), e = s + 8h
    base_r = monte_carlo_range_spec() # s = rand(6-12), e = rand(14-20)
    any_r = monte_carlo_range_spec((0,1440), (0,1440)) # s, e = rand(0-23:59) | e >= s + 15m
    base_8.to_csv('base_8.csv', index=False)
    base_r.to_csv('base_r.csv', index=False)

if __name__ == "__main__":
    main()
