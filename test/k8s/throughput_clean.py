#!/usr/bin/env python3
"""
Clean throughput visualization without explanation bubbles.
"""

import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
import numpy as np

# ============================================================================
# ACTUAL TEST DATA
# ============================================================================

baseline_preemptions = [76, 83, 90, 103, 176]
baseline_total = 400

cedana_checkpoints = [78, 97, 129, 170, 179]
cedana_total = 299

num_jobs = 5
time_saved = baseline_total - cedana_total
efficiency = (time_saved / baseline_total) * 100

# Model queue depth
def model_queue_depth(preemption_times, total_time, use_checkpoint, num_jobs=5):
    timeline = list(range(0, total_time + 1))
    queue_depth = []
    num_nodes = 2
    initial_queue = num_jobs - num_nodes

    for t in timeline:
        if use_checkpoint:
            completed = sum(1 for ct in cedana_checkpoints if t > ct + 20)
            queue = max(0, initial_queue - completed)
        else:
            completed = sum(1 for pt in preemption_times if t > pt + 80)
            queue = max(0, initial_queue - completed)
        queue_depth.append(queue)

    return timeline, queue_depth

baseline_time, baseline_queue = model_queue_depth(
    baseline_preemptions, baseline_total, use_checkpoint=False)
cedana_time, cedana_queue = model_queue_depth(
    cedana_checkpoints, cedana_total, use_checkpoint=True)

baseline_queue_time = sum(baseline_queue)
cedana_queue_time = sum(cedana_queue)
queue_time_saved = baseline_queue_time - cedana_queue_time

# ============================================================================
# Create Clean Visualization
# ============================================================================

fig = plt.figure(figsize=(18, 10))
gs = fig.add_gridspec(3, 3, hspace=0.4, wspace=0.4, top=0.92, bottom=0.08)

baseline_color = '#e74c3c'
cedana_color = '#2ecc71'
preempt_color = '#e67e22'
checkpoint_color = '#3498db'

# ============================================================================
# 1. Queue Depth Over Time
# ============================================================================
ax1 = fig.add_subplot(gs[0, :])

ax1.fill_between(baseline_time, 0, baseline_queue,
                 color=baseline_color, alpha=0.3, label='Baseline')
ax1.fill_between(cedana_time, 0, cedana_queue,
                 color=cedana_color, alpha=0.3, label='Cedana')

ax1.plot(baseline_time, baseline_queue, color=baseline_color, linewidth=3, alpha=0.9)
ax1.plot(cedana_time, cedana_queue, color=cedana_color, linewidth=3, alpha=0.9)

ax1.set_xlabel('Time (seconds)', fontsize=13, fontweight='bold')
ax1.set_ylabel('Jobs in Queue', fontsize=13, fontweight='bold')
ax1.set_title('Queue Depth Over Time', fontsize=15, fontweight='bold', pad=15)
ax1.legend(fontsize=12, loc='upper right', framealpha=0.95)
ax1.grid(True, alpha=0.3, linewidth=1)
ax1.set_xlim(0, baseline_total + 10)
ax1.set_ylim(-0.1, max(max(baseline_queue), max(cedana_queue)) + 0.3)

# ============================================================================
# 2. Timeline with Events
# ============================================================================
ax2 = fig.add_subplot(gs[1, :])

ax2.barh(1, baseline_total, height=0.3, left=0, color=baseline_color, alpha=0.4, edgecolor='black', linewidth=1.5)
ax2.barh(0, cedana_total, height=0.3, left=0, color=cedana_color, alpha=0.4, edgecolor='black', linewidth=1.5)

for t in baseline_preemptions:
    ax2.scatter(t, 1, s=200, c=preempt_color, marker='x', linewidths=3, zorder=3)
    ax2.text(t, 1.25, f'{t}', ha='center', fontsize=9, fontweight='bold')

for t in cedana_checkpoints:
    ax2.scatter(t, 0, s=150, c=checkpoint_color, marker='o', zorder=3, edgecolors='black', linewidths=1)
    ax2.text(t, -0.25, f'{t}', ha='center', fontsize=9, fontweight='bold')

ax2.text(baseline_total + 5, 1, f'{baseline_total}s', va='center', fontsize=11, fontweight='bold')
ax2.text(cedana_total + 5, 0, f'{cedana_total}s', va='center', fontsize=11, fontweight='bold')

ax2.set_ylim(-0.5, 1.5)
ax2.set_xlim(0, baseline_total + 30)
ax2.set_yticks([0, 1])
ax2.set_yticklabels(['Cedana\n(checkpoint)', 'Baseline\n(no checkpoint)'], fontsize=11, fontweight='bold')
ax2.set_xlabel('Time (seconds)', fontsize=13, fontweight='bold')
ax2.set_title('Timeline: Preemption/Checkpoint Events', fontsize=15, fontweight='bold', pad=15)
ax2.grid(True, axis='x', alpha=0.3, linewidth=1)

legend_elements = [
    plt.Line2D([0], [0], marker='x', color='w', markerfacecolor=preempt_color,
               markersize=12, markeredgewidth=2.5, label='Preemption (restart 0%)'),
    plt.Line2D([0], [0], marker='o', color='w', markerfacecolor=checkpoint_color,
               markersize=10, markeredgecolor='black', label='Checkpoint (restore)')
]
ax2.legend(handles=legend_elements, loc='lower right', fontsize=10, framealpha=0.95)

# ============================================================================
# 3. Total Wall Time
# ============================================================================
ax3 = fig.add_subplot(gs[2, 0])

bars = ax3.bar(['Baseline', 'Cedana'],
               [baseline_total, cedana_total],
               color=[baseline_color, cedana_color],
               width=0.6, edgecolor='black', linewidth=2)

for bar in bars:
    height = bar.get_height()
    ax3.text(bar.get_x() + bar.get_width()/2., height + 8,
            f'{int(height)}s',
            ha='center', va='bottom', fontsize=14, fontweight='bold')

ax3.annotate('', xy=(1, cedana_total), xytext=(1, baseline_total),
            arrowprops=dict(arrowstyle='<->', color='black', lw=2.5))
ax3.text(1.28, (baseline_total + cedana_total) / 2,
         f'{time_saved}s\n({efficiency:.1f}%)',
         va='center', fontsize=11, fontweight='bold',
         bbox=dict(boxstyle='round,pad=0.5', facecolor='gold', alpha=0.8, edgecolor='black', linewidth=1.5))

ax3.set_ylabel('Wall Time (seconds)', fontsize=12, fontweight='bold')
ax3.set_title('Total Time to Complete', fontsize=14, fontweight='bold', pad=12)
ax3.grid(True, axis='y', alpha=0.3)
ax3.set_ylim(0, baseline_total * 1.2)

# ============================================================================
# 4. Queue Time
# ============================================================================
ax4 = fig.add_subplot(gs[2, 1])

bars = ax4.bar(['Baseline', 'Cedana'],
               [baseline_queue_time, cedana_queue_time],
               color=[baseline_color, cedana_color],
               width=0.6, edgecolor='black', linewidth=2)

for bar in bars:
    height = bar.get_height()
    ax4.text(bar.get_x() + bar.get_width()/2., height + 12,
            f'{int(height)}',
            ha='center', va='bottom', fontsize=14, fontweight='bold')

ax4.annotate('', xy=(1, cedana_queue_time), xytext=(1, baseline_queue_time),
            arrowprops=dict(arrowstyle='<->', color='black', lw=2.5))
ax4.text(1.28, (baseline_queue_time + cedana_queue_time) / 2,
         f'{int(queue_time_saved)}',
         va='center', fontsize=11, fontweight='bold',
         bbox=dict(boxstyle='round,pad=0.5', facecolor='gold', alpha=0.8, edgecolor='black', linewidth=1.5))

ax4.set_ylabel('Queue Time (job-seconds)', fontsize=12, fontweight='bold')
ax4.set_title('Cumulative Queue Time', fontsize=14, fontweight='bold', pad=12)
ax4.grid(True, axis='y', alpha=0.3)

# ============================================================================
# 5. Event Distribution
# ============================================================================
ax5 = fig.add_subplot(gs[2, 2])

bins = [0, 50, 100, 150, 200]
ax5.hist([baseline_preemptions, cedana_checkpoints], bins=bins,
         label=['Baseline', 'Cedana'],
         color=[baseline_color, cedana_color], alpha=0.75,
         edgecolor='black', linewidth=1.5)

ax5.set_xlabel('Event Time (seconds)', fontsize=11, fontweight='bold')
ax5.set_ylabel('Count', fontsize=11, fontweight='bold')
ax5.set_title('Event Distribution', fontsize=14, fontweight='bold', pad=12)
ax5.legend(fontsize=10, framealpha=0.95)
ax5.grid(True, axis='y', alpha=0.3)

# ============================================================================
# Main title
# ============================================================================
fig.suptitle(
    f'Cedana Throughput Efficiency: {num_jobs} Concurrent Jobs\n{efficiency:.1f}% Improvement ({time_saved}s saved)',
    fontsize=16, fontweight='bold', y=0.96)

# Save
output_file = '/home/nravic/go/src/github.com/cedana/cedana/test/k8s/throughput_clean.png'
plt.savefig(output_file, dpi=300, bbox_inches='tight', facecolor='white')
print(f"✅ Clean visualization saved: {output_file}")
print(f"\n📊 Results:")
print(f"   Baseline: {baseline_total}s")
print(f"   Cedana:   {cedana_total}s")
print(f"   Saved:    {time_saved}s ({efficiency:.1f}%)")
print(f"   Queue time saved: {queue_time_saved} job-seconds")
