#!/usr/bin/env python3
"""
Visualize Cedana throughput efficiency test results.
Compares baseline (no checkpointing) vs Cedana (with checkpointing).
"""

import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
import numpy as np

# Test data
baseline_preemptions = [76, 83, 90, 103, 176]  # seconds when jobs were preempted
baseline_total = 400  # total wall time (seconds)

cedana_checkpoints = [78, 97, 129, 170, 179]  # seconds when jobs were checkpointed
cedana_total = 299  # total wall time (seconds)

# Number of jobs
num_jobs = len(baseline_preemptions)

# Calculate metrics
time_saved = baseline_total - cedana_total
efficiency = (time_saved / baseline_total) * 100

print(f"📊 Throughput Analysis:")
print(f"  Jobs: {num_jobs}")
print(f"  Baseline total: {baseline_total}s")
print(f"  Cedana total: {cedana_total}s")
print(f"  Time saved: {time_saved}s ({efficiency:.1f}% improvement)")
print()

# Create figure with subplots
fig = plt.figure(figsize=(16, 10))
gs = fig.add_gridspec(3, 2, hspace=0.3, wspace=0.3)

# Color scheme
baseline_color = '#e74c3c'  # red
cedana_color = '#2ecc71'    # green
preempt_color = '#e67e22'   # orange
checkpoint_color = '#3498db' # blue

# ============================================================================
# 1. Timeline Comparison - Preemption/Checkpoint Events
# ============================================================================
ax1 = fig.add_subplot(gs[0, :])

# Baseline preemptions
for i, time in enumerate(baseline_preemptions):
    ax1.scatter(time, 1, s=200, c=preempt_color, marker='x', linewidths=3, zorder=3)
    ax1.text(time, 1.15, f'{time}s', ha='center', fontsize=9, fontweight='bold')

# Cedana checkpoints
for i, time in enumerate(cedana_checkpoints):
    ax1.scatter(time, 0, s=200, c=checkpoint_color, marker='o', zorder=3)
    ax1.text(time, -0.15, f'{time}s', ha='center', fontsize=9, fontweight='bold')

# Timeline bars
ax1.barh(1, baseline_total, height=0.3, left=0, color=baseline_color, alpha=0.3)
ax1.barh(0, cedana_total, height=0.3, left=0, color=cedana_color, alpha=0.3)

# Mark total times at end
ax1.text(baseline_total + 5, 1, f'Total: {baseline_total}s', va='center', fontsize=11, fontweight='bold')
ax1.text(cedana_total + 5, 0, f'Total: {cedana_total}s', va='center', fontsize=11, fontweight='bold')

ax1.set_ylim(-0.5, 1.5)
ax1.set_xlim(0, baseline_total + 40)
ax1.set_yticks([0, 1])
ax1.set_yticklabels(['Cedana\n(w/ checkpoint)', 'Baseline\n(no checkpoint)'], fontsize=11)
ax1.set_xlabel('Time (seconds)', fontsize=12, fontweight='bold')
ax1.set_title('Timeline: Job Preemption/Checkpoint Events', fontsize=14, fontweight='bold', pad=15)
ax1.grid(True, axis='x', alpha=0.3)

# Legend
legend_elements = [
    mpatches.Patch(color=baseline_color, alpha=0.3, label='Baseline duration'),
    mpatches.Patch(color=cedana_color, alpha=0.3, label='Cedana duration'),
    plt.Line2D([0], [0], marker='x', color='w', markerfacecolor=preempt_color,
               markersize=12, markeredgewidth=2, label='Preemption (restart from 0%)'),
    plt.Line2D([0], [0], marker='o', color='w', markerfacecolor=checkpoint_color,
               markersize=10, label='Checkpoint (restore progress)')
]
ax1.legend(handles=legend_elements, loc='upper right', fontsize=10)

# ============================================================================
# 2. Total Time Comparison (Bar Chart)
# ============================================================================
ax2 = fig.add_subplot(gs[1, 0])

bars = ax2.bar(['Baseline\n(no checkpoint)', 'Cedana\n(w/ checkpoint)'],
               [baseline_total, cedana_total],
               color=[baseline_color, cedana_color],
               width=0.6)

# Add value labels on bars
for bar in bars:
    height = bar.get_height()
    ax2.text(bar.get_x() + bar.get_width()/2., height,
            f'{int(height)}s',
            ha='center', va='bottom', fontsize=14, fontweight='bold')

# Add improvement annotation
ax2.annotate('', xy=(1, cedana_total), xytext=(1, baseline_total),
            arrowprops=dict(arrowstyle='<->', color='black', lw=2))
ax2.text(1.15, (baseline_total + cedana_total) / 2,
         f'{time_saved}s saved\n({efficiency:.1f}%)',
         va='center', fontsize=11, fontweight='bold',
         bbox=dict(boxstyle='round', facecolor='wheat', alpha=0.5))

ax2.set_ylabel('Total Wall Time (seconds)', fontsize=12, fontweight='bold')
ax2.set_title('Total Completion Time Comparison', fontsize=14, fontweight='bold', pad=15)
ax2.set_ylim(0, baseline_total * 1.15)
ax2.grid(True, axis='y', alpha=0.3)

# ============================================================================
# 3. Preemption Time Distribution (Histogram)
# ============================================================================
ax3 = fig.add_subplot(gs[1, 1])

bins = [0, 50, 100, 150, 200, 250]
ax3.hist([baseline_preemptions, cedana_checkpoints], bins=bins,
         label=['Baseline preemptions', 'Cedana checkpoints'],
         color=[baseline_color, cedana_color], alpha=0.7, edgecolor='black')

ax3.set_xlabel('Time of Preemption/Checkpoint (seconds)', fontsize=12, fontweight='bold')
ax3.set_ylabel('Number of Events', fontsize=12, fontweight='bold')
ax3.set_title('Distribution of Preemption/Checkpoint Times', fontsize=14, fontweight='bold', pad=15)
ax3.legend(fontsize=10)
ax3.grid(True, axis='y', alpha=0.3)

# ============================================================================
# 4. Cumulative Jobs Completed Over Time (Simulated)
# ============================================================================
ax4 = fig.add_subplot(gs[2, :])

# For baseline: jobs restart from scratch after preemption
# Assume each job takes ~80s to complete without preemption
# When preempted, it restarts from 0%

# Create timeline
timeline = np.arange(0, baseline_total + 1)

# Baseline: Jobs complete after preemption, restarting from 0
# Simplified model: after preemption, job takes another 80s to complete
baseline_completion_times = []
for preempt_time in sorted(baseline_preemptions):
    # Job completes ~80s after preemption (restart from 0%)
    completion = preempt_time + 80
    baseline_completion_times.append(completion)

baseline_completion_times = sorted(baseline_completion_times)
baseline_cumulative = []
for t in timeline:
    completed = sum(1 for ct in baseline_completion_times if ct <= t)
    baseline_cumulative.append(completed)

# Cedana: Jobs complete after checkpoint/restore
# Simplified model: after checkpoint, job resumes and completes ~20s later (saves 60s)
cedana_completion_times = []
for ckpt_time in sorted(cedana_checkpoints):
    # Job completes ~20s after checkpoint (restored progress)
    completion = ckpt_time + 20
    cedana_completion_times.append(completion)

cedana_completion_times = sorted(cedana_completion_times)
cedana_timeline = np.arange(0, cedana_total + 1)
cedana_cumulative = []
for t in cedana_timeline:
    completed = sum(1 for ct in cedana_completion_times if ct <= t)
    cedana_cumulative.append(completed)

# Plot cumulative completions
ax4.plot(timeline, baseline_cumulative, color=baseline_color, linewidth=3,
         label='Baseline (jobs restart from 0%)', marker='', linestyle='-')
ax4.plot(cedana_timeline, cedana_cumulative, color=cedana_color, linewidth=3,
         label='Cedana (jobs restore progress)', marker='', linestyle='-')

# Mark final completion points
ax4.scatter([baseline_total], [num_jobs], s=200, c=baseline_color, marker='o',
           zorder=5, edgecolors='black', linewidths=2)
ax4.scatter([cedana_total], [num_jobs], s=200, c=cedana_color, marker='o',
           zorder=5, edgecolors='black', linewidths=2)

ax4.axhline(y=num_jobs, color='gray', linestyle='--', alpha=0.5, linewidth=1)
ax4.text(5, num_jobs + 0.15, f'{num_jobs} jobs total', fontsize=10, style='italic')

ax4.set_xlabel('Time (seconds)', fontsize=12, fontweight='bold')
ax4.set_ylabel('Jobs Completed', fontsize=12, fontweight='bold')
ax4.set_title('Cumulative Job Completion Over Time (Estimated)', fontsize=14, fontweight='bold', pad=15)
ax4.legend(fontsize=11, loc='lower right')
ax4.grid(True, alpha=0.3)
ax4.set_xlim(0, baseline_total + 10)
ax4.set_ylim(0, num_jobs + 0.5)

# ============================================================================
# Main title
# ============================================================================
fig.suptitle(f'Cedana Throughput Efficiency Report\n{num_jobs} Concurrent Jobs - {efficiency:.1f}% Improvement',
             fontsize=16, fontweight='bold', y=0.98)

# Save figure
output_file = '/home/nravic/go/src/github.com/cedana/cedana/test/k8s/throughput_report.png'
plt.savefig(output_file, dpi=300, bbox_inches='tight')
print(f"✅ Graph saved to: {output_file}")

# Display
plt.show()
