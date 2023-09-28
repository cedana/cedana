import sqlite3
import os
import matplotlib.pyplot as plt
import matplotlib.colors as mcolors
import pandas as pd
import psutil

# Connect to the SQLite database file
homeDir = os.getenv("HOME")

db_file = f'{homeDir}/.cedana/benchmarking.db'

# Create connection to the benchmarking database
conn = sqlite3.connect(db_file)
cursor = conn.cursor()

benchmarkTable = 'benchmark_results'

# Fetch all rows from the table
dumpQuery = f"SELECT * FROM {benchmarkTable} WHERE cmd_type = 'dump'"
restoreQuery = f"SELECT * FROM {benchmarkTable} WHERE cmd_type = 'restore'"


def AnalyzeBenchmarks(query, title):
    mainDf = pd.read_sql_query(query, conn)
    print(mainDf.head())

    # Create a dictionary to map unique categories to colors
    unique_categories = mainDf['process_name'].unique()
    print(unique_categories)
    num_unique_categories = len(unique_categories)
    color_map = dict(zip(unique_categories, mcolors.TABLEAU_COLORS))

    # # Create a numeric mapping for process names
    process_name_mapping = {name: i for i,
                            name in enumerate(unique_categories)}
    listOfDfs = []
    for category in unique_categories:
        listOfDfs.append(mainDf[mainDf['process_name'] == category])

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

    # Convert process names to numeric values for coloring
    mainDf['process_color'] = mainDf['process_name'].map(process_name_mapping)

    # Create a figure and a 3D Axes
    fig = plt.figure()
    ax = fig.add_subplot(111, projection='3d')

    memory = psutil.virtual_memory().total

    # Create a plot using pandas and matplotlib
    scatter = ax.scatter(
        (mainDf['elapsed_time_ms'] / 1000),
        (mainDf['total_memory_used'] / memory) * 100,
        mainDf['file_size']*1e-6,
        c=mainDf['process_color'],  # Use process_color for coloring
        cmap=plt.cm.tab10,
        marker='o',
    )

    legend_labels = [name for i, name in sorted(
        process_name_mapping.items(), key=lambda x: x[1])]

    handles, _ = scatter.legend_elements(prop="colors")
    legend = plt.legend(handles, unique_categories,
                        title="Process Name", loc="upper right")

    plt.ylabel('Total memory allocation (%)')
    plt.xlabel('Total CPU time (s)')
    ax.set_zlabel('Total Disk allocation (MB)')

    plt.title(f'{title} Benchmark Analysis')


AnalyzeBenchmarks(dumpQuery, "dump")
AnalyzeBenchmarks(restoreQuery, "restore")
plt.savefig("output.png")
# Close the connection
conn.close()
