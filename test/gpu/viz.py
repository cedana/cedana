import matplotlib.pyplot as plt
from matplotlib.animation import FuncAnimation
import traceback

def parse_data(filename):
    sizes, times, rates = [], [], []
    try:
        with open(filename, 'r') as file:
            for line in file:
                if line.startswith('Sorted'):
                    parts = line.split()
                    sizes.append(int(parts[1]))
                    times.append(float(parts[4]))
                    rate_str = parts[6].strip('()')
                    rates.append(float(rate_str))
    except Exception:
        traceback.print_exc()
    return sizes, times, rates

def animate(i, filename, ax1, ax2):
    sizes, times, rates = parse_data(filename)
    ax1.clear()
    ax2.clear()
    
    ax1.set_xlabel('Number of Elements')
    ax1.set_ylabel('Time (ms)', color='tab:green')
    ax2.set_ylabel('Mega-elements/sec', color='tab:blue')

    if sizes:
        ax1.plot(sizes, times, color='tab:green')
        ax2.plot(sizes, rates, color='tab:blue')

# Setup plot
fig, ax1 = plt.subplots()
ax2 = ax1.twinx()
plt.title('GPU Quicksort Performance')

filename = '/var/log/cedana-output.log'  # Replace with your actual file name

# Initialize plot
ax1.set_xlabel('Number of Elements')
ax1.set_ylabel('Time (ms)', color='tab:green')
ax2.set_ylabel('Mega-elements/sec', color='tab:blue')

# Creating animation
ani = FuncAnimation(fig, animate, fargs=(filename, ax1, ax2), interval=100)
plt.show()

