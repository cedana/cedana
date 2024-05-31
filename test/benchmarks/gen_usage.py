import json
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd
import random

# machine is always on with no cedana control
def base():
    usage = pd.DataFrame({'time': range(1440)})
    usage['utilization'] = True
    usage['suspend'] = False
    usage['migrate'] = False
    usage['restore'] = False
    return usage

def monte_carlo(s: int, e: int):
    usage = pd.DataFrame({'time': range(1440)})
    usage['utilization'] = usage['time'].apply(lambda t: t >= s and t < e)
    usage['suspend'] = usage['time'] == e
    usage['migrate'] = usage['time'] == s
    usage['restore'] = usage['time'] == (s + 1)
    return usage

# user works hrs_worked hours a day based on specified start_range
def monte_carlo_len_spec(start_range=(360,720), hrs_worked=8):
    s = random.randint(start_range[0], start_range[1])
    e = s + hrs_worked*60
    return monte_carlo(s, e)

# user works at times based on specified start_range and end_range
def monte_carlo_range_spec(start_range=(360, 720), end_range=(840, 1200)):
    a = random.randint(start_range[0], start_range[1])
    b = random.randint(end_range[0], end_range[1])
    while abs(a-b) < 15: # work minimum 15 minutes
        a = random.randint(start_range[0], start_range[1])
        b = random.randint(end_range[0], end_range[1])
    s = min(a, b)
    e = max(a, b)
    return monte_carlo(s, e)

# on for n minutes, off for n minutes
def on_and_off(n=15):
    usage = pd.DataFrame({'time': range(1440)})
    usage['utilization'] = usage['time'] % (2 * n) < n
    usage['suspend'] = usage['time'] % (2 * n) == n-1
    usage['migrate'] = usage['time'] % (2 * n) == 0
    usage['restore'] = usage['time'] % (2 * n) == 1
    return usage

def main():
    # s = start, e = end
    # time in 24h format
    base_8 = monte_carlo_len_spec((540,540)) # s = 9, e = 17
    range_8 = monte_carlo_len_spec() # s = rand(6-12), e = s + 8h
    base_r = monte_carlo_range_spec() # s = rand(6-12), e = rand(14-20)
    any_r = monte_carlo_range_spec((0,1440), (0,1440)) # s, e = rand(0-23:59) | e >= s + 15m
    on_off = on_and_off(5)
    base_8.to_csv('base_8.csv', index=False)
    base_r.to_csv('base_r.csv', index=False)
    on_off.to_csv('on_off.csv', index=False)

if __name__ == "__main__":
    main()
