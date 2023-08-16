import sqlite3
import os
import matplotlib.pyplot as plt
import matplotlib.colors as mcolors
import pandas as pd
import math

# Connect to the SQLite database file
homeDir = os.getenv("HOME")

db_file = f'{homeDir}/.cedana/benchmarking.db'
conn = sqlite3.connect(db_file)
cursor = conn.cursor()

# Replace 'your_table_name' with the actual table name
benchmarkTable = 'benchmarks'

# Fetch all rows from the table
query = f"SELECT * FROM {benchmarkTable}"
mainDf = pd.read_sql_query(query, conn)

# Close the connection
conn.close()

# Create a dictionary to map unique categories to colors
unique_categories = mainDf['process_name'].unique()
print(unique_categories)
num_unique_categories = len(unique_categories)
color_map = dict(zip(unique_categories, mcolors.TABLEAU_COLORS))

# # Create a numeric mapping for process names
process_name_mapping = {name: i for i, name in enumerate(unique_categories)}
listOfDfs = []
for category in unique_categories:
    listOfDfs.append(mainDf[mainDf['process_name'] == category])
# loopDf = df[df['process_name'] == unique_categories[0]]
# serverDf = df[df['process_name'] == unique_categories[1]]
# pytorchDf = df[df['process_name'] == unique_categories[2]]

for df in listOfDfs:
    df['elapsed_time_ms'] = df['elapsed_time_ms']/1000
    df['total_memory_used'] = (df['total_memory_used'] * 1e-6 / 4000)*100
    print(
        f"Mean of cpu time allocation of {df['process_name'].iloc[0]}: {df['elapsed_time_ms'].mean()} seconds")
    print(
        f"Mean of memory allocation of {df['process_name'].iloc[0]}: {df['total_memory_used'].mean()} mb")
    print("")
    print(
        f"Std of cpu time allocation of {df['process_name'].iloc[0]}: {df['elapsed_time_ms'].std()} seconds")
    print(
        f"Std of memory allocation of {df['process_name'].iloc[0]}: {df['total_memory_used'].std()} mb")
    print("")


# pytorchStd = (pytorchDf['elapsed_time_ms']/1000).std()
# pytorchMemoryStd = ((pytorchDf['total_memory_used'] * 1e-6 / 4000)*100).std()
# print(f'pytorch std: {pytorchStd} seconds')
# print(f'pytorch memory std: {pytorchMemoryStd}%')
# print("")
# serverStd = (serverDf['elapsed_time_ms']/1000).std()
# serverMemoryStd = ((serverDf['total_memory_used'] * 1e-6 / 4000)*100).std()
# serverMemoryMean = ((serverDf['total_memory_used'] * 1e-6 / 4000)*100).mean()
# print(f'server std: {serverStd} seconds')
# print(f'server memory std: {serverMemoryStd}%')
# print(f'server memory mean: {serverMemoryMean}%')
# print("")
# loopStd = (loopDf['elapsed_time_ms']/1000).std()
# loopMemoryStd = ((loopDf['total_memory_used'] * 1e-6 / 4000)*100).std()
# loopMemoryMean = ((loopDf['total_memory_used'] * 1e-6 / 4000)*100).mean()
# print(f'server std: {loopStd} seconds')
# print(f'server memory std: {loopMemoryStd}%')
# print(f'loop memory mean: {loopMemoryMean}%')

# changeInStdOverMean = (serverMemoryStd - loopMemoryStd) / (serverMemoryMean - loopMemoryMean)
# print(f'change in std over mean: {changeInStdOverMean}')


# Convert process names to numeric values for coloring
mainDf['process_color'] = mainDf['process_name'].map(process_name_mapping)

# Create a figure and a 3D Axes
fig = plt.figure()
ax = fig.add_subplot(111, projection='3d')


# Create a plot using pandas and matplotlib
scatter = ax.scatter(
    (mainDf['elapsed_time_ms'] / 1000),  # X-axis
    (mainDf['total_memory_used'] * 1e-6 / 4000) * 100,  # Y-axis
    mainDf['file_size']*1e-6,  # Z-axis (color)
    c=mainDf['process_color'],  # Use process_color for coloring
    cmap=plt.cm.tab10,
    marker='o',
)

# Get the legend labels using the process_name_mapping
legend_labels = [name for i, name in sorted(
    process_name_mapping.items(), key=lambda x: x[1])]

# Create a legend with the specified labels and colors
handles, _ = scatter.legend_elements(prop="colors")
legend = plt.legend(handles, unique_categories,
                    title="Process Name", loc="upper right")

plt.xlabel('Total Memory Allocation (%)')
plt.ylabel('Total CPU Time Allocation (s)')
plt.title('Checkpointing Benchmark Analysis')
plt.show()
plt.savefig('benchmark_analysis.png')
