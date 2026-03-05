#!/usr/bin/env python3
"""
Queue Depth Analysis: Shows how checkpointing prevents queue buildup.

Key insight: When jobs restart from scratch (baseline), they take longer to complete,
causing queued jobs to wait longer. With checkpointing (Cedana), jobs complete faster
after preemption, so the queue clears faster.
"""

import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
import numpy as np

# ============================================================================
# Realistic Scenario: 10 jobs on 3 nodes, 1-hour test
# ============================================================================

num_jobs = 10
num_nodes = 3  # Only 3 jobs can run at once → 7 jobs must queue
job_runtime = 3600  # 60 minutes per job if uninterrupted

# Preemption times (in seconds from start)
# These are when the running jobs get preempted
preemption_times = [540, 720, 1080, 1260, 1620, 1980, 2340, 2700, 3060, 3420]

checkpoint_overhead = 15  # seconds
restore_overhead = 10     # seconds

print(f"📊 Queue Depth Analysis")
print(f"  Scenario: {num_jobs} jobs competing for {num_nodes} nodes")
print(f"  Job runtime: {job_runtime}s ({job_runtime/60:.0f} min)")
print(f"  Preemption times: {len(preemption_times)} events")
print()

# ============================================================================
# Baseline Simulation: Jobs restart from 0% after preemption
# ============================================================================

class Job:
    def __init__(self, job_id):
        self.id = job_id
        self.state = 'queued'  # queued, running, completed
        self.progress = 0.0  # 0-100%
        self.start_time = None
        self.completion_time = None
        self.preempted = False
        self.total_runtime = 0

def simulate_baseline(num_jobs, num_nodes, job_runtime, preemption_schedule):
    """Baseline: Jobs restart from 0% when preempted"""
    jobs = [Job(i) for i in range(num_jobs)]
    timeline = []
    queue_depths = []
    running_counts = []

    # Preemption schedule: {time: [job_indices]}
    preempt_dict = {}
    for i, t in enumerate(preemption_schedule):
        if t not in preempt_dict:
            preempt_dict[t] = []
        # Preempt running jobs in order
        preempt_dict[t].append(i % num_nodes)

    # Start first 3 jobs
    for i in range(min(num_nodes, num_jobs)):
        jobs[i].state = 'running'
        jobs[i].start_time = 0

    t = 0
    dt = 5  # 5-second time steps
    max_time = 10000  # safety limit

    while t < max_time:
        # Update progress for running jobs
        for job in jobs:
            if job.state == 'running':
                job.progress += (dt / job_runtime) * 100
                if job.progress >= 100:
                    job.state = 'completed'
                    job.completion_time = t
                    job.progress = 100

        # Handle preemptions
        if t in preempt_dict:
            for job_idx in preempt_dict[t]:
                if job_idx < len(jobs) and jobs[job_idx].state == 'running':
                    # Preempt: lose all progress, go back to queue
                    jobs[job_idx].state = 'queued'
                    jobs[job_idx].progress = 0
                    jobs[job_idx].preempted = True

        # Start queued jobs on available nodes
        running = [j for j in jobs if j.state == 'running']
        queued = [j for j in jobs if j.state == 'queued']

        available_slots = num_nodes - len(running)
        for i in range(min(available_slots, len(queued))):
            queued[i].state = 'running'
            queued[i].start_time = t

        # Record metrics
        timeline.append(t)
        queue_depths.append(len([j for j in jobs if j.state == 'queued']))
        running_counts.append(len([j for j in jobs if j.state == 'running']))

        # Check if all jobs completed
        if all(j.state == 'completed' for j in jobs):
            break

        t += dt

    total_time = t
    return timeline, queue_depths, running_counts, total_time

# ============================================================================
# Cedana Simulation: Jobs restore progress after preemption
# ============================================================================

def simulate_cedana(num_jobs, num_nodes, job_runtime, preemption_schedule):
    """Cedana: Jobs restore progress when preempted"""
    jobs = [Job(i) for i in range(num_jobs)]
    timeline = []
    queue_depths = []
    running_counts = []

    # Preemption schedule
    preempt_dict = {}
    for i, t in enumerate(preemption_schedule):
        if t not in preempt_dict:
            preempt_dict[t] = []
        preempt_dict[t].append(i % num_nodes)

    # Start first 3 jobs
    for i in range(min(num_nodes, num_jobs)):
        jobs[i].state = 'running'
        jobs[i].start_time = 0

    t = 0
    dt = 5
    max_time = 10000

    while t < max_time:
        # Update progress for running jobs
        for job in jobs:
            if job.state == 'running':
                job.progress += (dt / job_runtime) * 100
                if job.progress >= 100:
                    job.state = 'completed'
                    job.completion_time = t
                    job.progress = 100

        # Handle preemptions (with checkpoint/restore)
        if t in preempt_dict:
            for job_idx in preempt_dict[t]:
                if job_idx < len(jobs) and jobs[job_idx].state == 'running':
                    # Checkpoint: save progress, go to queue
                    # Progress is preserved!
                    jobs[job_idx].state = 'queued'
                    jobs[job_idx].preempted = True

        # Start queued jobs on available nodes
        running = [j for j in jobs if j.state == 'running']
        queued = [j for j in jobs if j.state == 'queued']

        available_slots = num_nodes - len(running)
        for i in range(min(available_slots, len(queued))):
            queued[i].state = 'running'
            queued[i].start_time = t

        # Record metrics
        timeline.append(t)
        queue_depths.append(len([j for j in jobs if j.state == 'queued']))
        running_counts.append(len([j for j in jobs if j.state == 'running']))

        # Check if all jobs completed
        if all(j.state == 'completed' for j in jobs):
            break

        t += dt

    total_time = t
    return timeline, queue_depths, running_counts, total_time

# Run simulations
print("Running baseline simulation...")
baseline_time, baseline_queue, baseline_running, baseline_total = \
    simulate_baseline(num_jobs, num_nodes, job_runtime, preemption_times)

print("Running Cedana simulation...")
cedana_time, cedana_queue, cedana_running, cedana_total = \
    simulate_cedana(num_jobs, num_nodes, job_runtime, preemption_times)

time_saved = baseline_total - cedana_total
efficiency = (time_saved / baseline_total) * 100

print(f"\n✅ Simulation Complete:")
print(f"  Baseline: {baseline_total}s ({baseline_total/60:.1f} min)")
print(f"  Cedana: {cedana_total}s ({cedana_total/60:.1f} min)")
print(f"  Time saved: {time_saved}s ({time_saved/60:.1f} min, {efficiency:.1f}%)")

# ============================================================================
# Create Comprehensive Visualizations
# ============================================================================

fig = plt.figure(figsize=(20, 14))
gs = fig.add_gridspec(4, 2, hspace=0.4, wspace=0.3, top=0.94, bottom=0.05)

# Colors
baseline_color = '#e74c3c'
cedana_color = '#2ecc71'
preempt_color = '#e67e22'
queue_color = '#9b59b6'

# Convert to minutes
baseline_time_min = [t/60 for t in baseline_time]
cedana_time_min = [t/60 for t in cedana_time]

# ============================================================================
# 1. MAIN GRAPH: Queue Depth Comparison
# ============================================================================
ax1 = fig.add_subplot(gs[0:2, :])

# Fill areas
ax1.fill_between(baseline_time_min, 0, baseline_queue,
                 color=baseline_color, alpha=0.4, label='Baseline queue depth')
ax1.fill_between(cedana_time_min, 0, cedana_queue,
                 color=cedana_color, alpha=0.4, label='Cedana queue depth')

# Lines
ax1.plot(baseline_time_min, baseline_queue, color=baseline_color, linewidth=3, alpha=0.9)
ax1.plot(cedana_time_min, cedana_queue, color=cedana_color, linewidth=3, alpha=0.9)

# Mark preemption events
for i, t in enumerate(preemption_times):
    if i == 0:
        ax1.axvline(x=t/60, color=preempt_color, linestyle=':', alpha=0.6, linewidth=2,
                   label='Preemption event')
    else:
        ax1.axvline(x=t/60, color=preempt_color, linestyle=':', alpha=0.6, linewidth=2)

# Mark when queues start diverging
diverge_idx = next((i for i in range(min(len(baseline_queue), len(cedana_queue)))
                   if abs(baseline_queue[i] - cedana_queue[i]) > 1), None)
if diverge_idx:
    diverge_time = baseline_time_min[diverge_idx]
    ax1.axvline(x=diverge_time, color='black', linestyle='--', linewidth=2, alpha=0.5)
    ax1.text(diverge_time, max(max(baseline_queue), max(cedana_queue)) * 0.9,
            'Queue buildup\nstarts diverging →',
            fontsize=11, ha='right', bbox=dict(boxstyle='round', facecolor='yellow', alpha=0.6))

# Annotations
max_baseline_queue = max(baseline_queue) if baseline_queue else 0
max_cedana_queue = max(cedana_queue) if cedana_queue else 0

ax1.text(baseline_total/60 * 0.6, max_baseline_queue * 0.7,
         f'Baseline: Queue backs up\nJobs restart from 0%\nMax queue: {max_baseline_queue} jobs',
         fontsize=12, fontweight='bold',
         bbox=dict(boxstyle='round', facecolor=baseline_color, alpha=0.5, edgecolor='black', linewidth=2))

ax1.text(cedana_total/60 * 0.4, max_cedana_queue * 0.4,
         f'Cedana: Queue clears faster\nJobs restore progress\nMax queue: {max_cedana_queue} jobs',
         fontsize=12, fontweight='bold',
         bbox=dict(boxstyle='round', facecolor=cedana_color, alpha=0.5, edgecolor='black', linewidth=2))

ax1.set_xlabel('Time (minutes)', fontsize=14, fontweight='bold')
ax1.set_ylabel('Number of Jobs Waiting in Queue', fontsize=14, fontweight='bold')
ax1.set_title('Queue Depth Over Time: The Compounding Effect of Checkpointing',
              fontsize=16, fontweight='bold', pad=20)
ax1.legend(fontsize=12, loc='upper left', framealpha=0.9)
ax1.grid(True, alpha=0.3, linewidth=1)
ax1.set_xlim(0, max(baseline_total, cedana_total)/60 + 5)
ax1.set_ylim(0, max(max_baseline_queue, max_cedana_queue) + 1)

# Add horizontal line for max queue
ax1.axhline(y=num_jobs - num_nodes, color='gray', linestyle='--', alpha=0.4, linewidth=1.5)
ax1.text(2, num_jobs - num_nodes + 0.3, f'Max possible queue ({num_jobs - num_nodes} jobs)',
         fontsize=10, style='italic', color='gray')

# ============================================================================
# 2. Cumulative Queue Time (Area Under Curve)
# ============================================================================
ax2 = fig.add_subplot(gs[2, 0])

# Calculate total queue time
baseline_queue_time = sum(baseline_queue) * 5 / 60  # 5s intervals → minutes
cedana_queue_time = sum(cedana_queue) * 5 / 60
queue_time_saved = baseline_queue_time - cedana_queue_time

bars = ax2.bar(['Baseline\n(restart from 0%)', 'Cedana\n(restore progress)'],
               [baseline_queue_time, cedana_queue_time],
               color=[baseline_color, cedana_color],
               width=0.6, edgecolor='black', linewidth=2)

# Value labels
for bar in bars:
    height = bar.get_height()
    ax2.text(bar.get_x() + bar.get_width()/2., height + 20,
            f'{int(height)}\njob-minutes',
            ha='center', va='bottom', fontsize=13, fontweight='bold')

# Savings annotation
if baseline_queue_time > cedana_queue_time:
    ax2.annotate('', xy=(1, cedana_queue_time), xytext=(1, baseline_queue_time),
                arrowprops=dict(arrowstyle='<->', color='black', lw=3))
    ax2.text(1.25, (baseline_queue_time + cedana_queue_time) / 2,
             f'{int(queue_time_saved)}\njob-min\nsaved',
             va='center', fontsize=12, fontweight='bold',
             bbox=dict(boxstyle='round', facecolor='gold', alpha=0.7, edgecolor='black', linewidth=2))

ax2.set_ylabel('Total Queue Time (job-minutes)', fontsize=13, fontweight='bold')
ax2.set_title('Cumulative Time Spent Waiting in Queue', fontsize=14, fontweight='bold', pad=15)
ax2.grid(True, axis='y', alpha=0.3)
ax2.set_ylim(0, baseline_queue_time * 1.25)

# ============================================================================
# 3. Total Wall Time Comparison
# ============================================================================
ax3 = fig.add_subplot(gs[2, 1])

bars = ax3.bar(['Baseline', 'Cedana'],
               [baseline_total/60, cedana_total/60],
               color=[baseline_color, cedana_color],
               width=0.6, edgecolor='black', linewidth=2)

# Value labels
for bar in bars:
    height = bar.get_height()
    ax3.text(bar.get_x() + bar.get_width()/2., height + 2,
            f'{height:.1f} min\n({int(height*60)}s)',
            ha='center', va='bottom', fontsize=13, fontweight='bold')

# Savings annotation
ax3.annotate('', xy=(1, cedana_total/60), xytext=(1, baseline_total/60),
            arrowprops=dict(arrowstyle='<->', color='black', lw=3))
ax3.text(1.25, (baseline_total/60 + cedana_total/60) / 2,
         f'{time_saved/60:.1f} min\nsaved\n({efficiency:.1f}%)',
         va='center', fontsize=12, fontweight='bold',
         bbox=dict(boxstyle='round', facecolor='gold', alpha=0.7, edgecolor='black', linewidth=2))

ax3.set_ylabel('Total Wall Time (minutes)', fontsize=13, fontweight='bold')
ax3.set_title('Time to Complete All Jobs', fontsize=14, fontweight='bold', pad=15)
ax3.grid(True, axis='y', alpha=0.3)
ax3.set_ylim(0, baseline_total/60 * 1.2)

# ============================================================================
# 4. Running Jobs Over Time (Resource Utilization)
# ============================================================================
ax4 = fig.add_subplot(gs[3, :])

ax4.plot(baseline_time_min, baseline_running, color=baseline_color,
         linewidth=2.5, label='Baseline: Running jobs', alpha=0.9)
ax4.plot(cedana_time_min, cedana_running, color=cedana_color,
         linewidth=2.5, label='Cedana: Running jobs', alpha=0.9)

ax4.axhline(y=num_nodes, color='black', linestyle='--', linewidth=2, alpha=0.6)
ax4.fill_between([0, max(baseline_total, cedana_total)/60], num_nodes, num_nodes + 0.5,
                 color='gray', alpha=0.2)
ax4.text(5, num_nodes + 0.15, f'Max capacity: {num_nodes} nodes', fontsize=11, fontweight='bold')

# Mark preemption events
for t in preemption_times[:3]:  # Just first few for clarity
    ax4.axvline(x=t/60, color=preempt_color, linestyle=':', alpha=0.4, linewidth=1.5)

ax4.set_xlabel('Time (minutes)', fontsize=14, fontweight='bold')
ax4.set_ylabel('Number of Running Jobs', fontsize=14, fontweight='bold')
ax4.set_title(f'Cluster Resource Utilization ({num_nodes} nodes available)',
              fontsize=16, fontweight='bold', pad=15)
ax4.legend(fontsize=12, loc='lower right')
ax4.grid(True, alpha=0.3)
ax4.set_xlim(0, max(baseline_total, cedana_total)/60 + 5)
ax4.set_ylim(0, num_nodes + 0.5)

# ============================================================================
# Main title
# ============================================================================
fig.suptitle(
    f'Cedana Throughput Efficiency: Queue Depth Analysis\n' +
    f'{num_jobs} Concurrent Jobs on {num_nodes} Nodes → {efficiency:.1f}% Improvement ({time_saved/60:.1f} min saved)',
    fontsize=18, fontweight='bold', y=0.98)

# Save
output_file = '/home/nravic/go/src/github.com/cedana/cedana/test/k8s/throughput_queue_analysis.png'
plt.savefig(output_file, dpi=300, bbox_inches='tight')
print(f"\n✅ Visualization saved: {output_file}")

# ============================================================================
# Print Summary Statistics
# ============================================================================
print(f"\n📈 QUEUE DEPTH INSIGHTS:")
print(f"\n  Max Queue Depth:")
print(f"    Baseline: {max_baseline_queue} jobs")
print(f"    Cedana:   {max_cedana_queue} jobs")
print(f"    Reduction: {max_baseline_queue - max_cedana_queue} jobs")

avg_baseline_queue = sum(baseline_queue) / len(baseline_queue) if baseline_queue else 0
avg_cedana_queue = sum(cedana_queue) / len(cedana_queue) if cedana_queue else 0

print(f"\n  Average Queue Depth:")
print(f"    Baseline: {avg_baseline_queue:.2f} jobs")
print(f"    Cedana:   {avg_cedana_queue:.2f} jobs")
if avg_baseline_queue > 0:
    print(f"    Reduction: {avg_baseline_queue - avg_cedana_queue:.2f} jobs ({(avg_baseline_queue - avg_cedana_queue)/avg_baseline_queue*100:.1f}%)")

print(f"\n  Total Queue Time:")
print(f"    Baseline: {baseline_queue_time:.0f} job-minutes")
print(f"    Cedana:   {cedana_queue_time:.0f} job-minutes")
print(f"    Saved:    {queue_time_saved:.0f} job-minutes")

print(f"\n  Overall Throughput:")
print(f"    Jobs completed: {num_jobs}")
print(f"    Time saved: {time_saved}s ({time_saved/60:.1f} min)")
print(f"    Efficiency gain: {efficiency:.1f}%")

# plt.show()  # Commented out to prevent blocking
