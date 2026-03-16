#!/usr/bin/env python3
"""
Cedana Throughput Test Visualization

Reads actual test results from results.json and generates comprehensive visualizations
showing how checkpointing prevents job failures when preemptions occur late in execution.
"""

import json
import sys
import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
import numpy as np
from pathlib import Path

# Configuration
RESULTS_FILE = Path("/tmp/throughput-test-state/results.json")
OUTPUT_FILE = Path(__file__).parent / "throughput_results.png"

def load_results(file_path):
    """Load test results from JSON file"""
    if not file_path.exists():
        print(f"❌ Results file not found: {file_path}")
        print("   Run the throughput test first to generate results.json")
        sys.exit(1)

    with open(file_path, 'r') as f:
        return json.load(f)

def main():
    print("📊 Cedana Throughput Test Visualization")
    print("=" * 60)

    # Load data
    data = load_results(RESULTS_FILE)

    test_config = data['test_config']
    baseline = data['baseline']
    cedana = data['cedana']

    print(f"\n📋 Test Configuration:")
    print(f"   Jobs: {test_config['num_jobs']}")
    print(f"   Job duration: {test_config['job_duration_sec']}s ({test_config['job_duration_sec']//60} min)")
    print(f"   Wall clock limit: {test_config['wall_clock_limit_sec']}s ({test_config['wall_clock_limit_sec']//60} min)")
    print(f"   Preemption interval: {test_config['preemption_interval_sec']}s ({test_config['preemption_interval_sec']//60:.1f} min)")

    print(f"\n📈 Results:")
    print(f"   Baseline: {baseline['summary']['completed']}/{baseline['summary']['total']} completed, "
          f"{baseline['summary']['failed']} failed ({baseline['summary']['completion_rate']:.1%})")
    print(f"   Cedana:   {cedana['summary']['completed']}/{cedana['summary']['total']} completed, "
          f"{cedana['summary']['failed']} failed ({cedana['summary']['completion_rate']:.1%})")

    # Calculate throughput metrics
    baseline_throughput = baseline['summary']['completed'] / baseline['total_wall_time_sec'] if baseline['total_wall_time_sec'] > 0 else 0
    cedana_throughput = cedana['summary']['completed'] / cedana['total_wall_time_sec'] if cedana['total_wall_time_sec'] > 0 else 0
    throughput_improvement = ((cedana_throughput - baseline_throughput) / baseline_throughput * 100) if baseline_throughput > 0 else 0

    time_saved = baseline['total_wall_time_sec'] - cedana['total_wall_time_sec']
    time_reduction = (time_saved / baseline['total_wall_time_sec'] * 100) if baseline['total_wall_time_sec'] > 0 else 0

    print(f"\n⏱️  Time:")
    print(f"   Baseline: {baseline['total_wall_time_sec']}s ({baseline['total_wall_time_sec']//60} min)")
    print(f"   Cedana:   {cedana['total_wall_time_sec']}s ({cedana['total_wall_time_sec']//60} min)")
    print(f"   Saved:    {time_saved}s ({time_saved//60} min, {time_reduction:.1f}% reduction)")

    print(f"\n🚀 Throughput:")
    print(f"   Baseline: {baseline_throughput:.6f} jobs/sec")
    print(f"   Cedana:   {cedana_throughput:.6f} jobs/sec")
    print(f"   Improvement: {throughput_improvement:.1f}%")

    # Create visualizations
    create_visualizations(data, test_config, baseline, cedana, time_saved, time_reduction, throughput_improvement)

    print(f"\n✅ Visualization saved: {OUTPUT_FILE}")

def create_visualizations(data, config, baseline, cedana, time_saved, time_reduction, throughput_improvement):
    """Create comprehensive visualization plots"""

    fig = plt.figure(figsize=(20, 10))
    gs = fig.add_gridspec(2, 3, hspace=0.6, wspace=0.35, top=0.86, bottom=0.08, left=0.08, right=0.95)

    # Colors
    baseline_completed_color = '#3498db'  # blue
    cedana_color = '#2ecc71'              # green
    failed_color = '#e74c3c'              # red
    preempt_color = '#e67e22'             # orange

    # ============================================================================
    # 1. Completion Rate Comparison (Main metric)
    # ============================================================================
    ax1 = fig.add_subplot(gs[0, 0])

    categories = ['Completed', 'Failed']
    baseline_data = [baseline['summary']['completed'], baseline['summary']['failed']]
    cedana_data = [cedana['summary']['completed'], cedana['summary']['failed']]

    x = np.arange(len(categories))
    width = 0.35

    bars1 = ax1.bar(x - width/2, baseline_data, width, label='Baseline',
                    color=[baseline_completed_color, failed_color], edgecolor='black', linewidth=2)
    bars2 = ax1.bar(x + width/2, cedana_data, width, label='Cedana',
                    color=[cedana_color, failed_color], edgecolor='black', linewidth=2)

    # Add value labels
    for bars in [bars1, bars2]:
        for bar in bars:
            height = bar.get_height()
            if height > 0:
                ax1.text(bar.get_x() + bar.get_width()/2., height,
                        f'{int(height)}',
                        ha='center', va='bottom', fontsize=13, fontweight='bold')

    ax1.set_ylabel('Number of Jobs', fontsize=12, fontweight='bold')
    ax1.set_title('Job Completion Rates', fontsize=14, fontweight='bold', pad=12)
    ax1.set_xticks(x)
    ax1.set_xticklabels(categories, fontsize=11)
    ax1.legend(fontsize=10, framealpha=0.95, edgecolor='black')
    ax1.grid(True, axis='y', alpha=0.3)
    ax1.set_ylim(0, max(max(baseline_data), max(cedana_data)) * 1.2)

    # ============================================================================
    # 2. Throughput Comparison (PRIMARY METRIC)
    # ============================================================================
    ax2 = fig.add_subplot(gs[0, 1])

    baseline_throughput = baseline['summary']['completed'] / baseline['total_wall_time_sec'] if baseline['total_wall_time_sec'] > 0 else 0
    cedana_throughput = cedana['summary']['completed'] / cedana['total_wall_time_sec'] if cedana['total_wall_time_sec'] > 0 else 0

    bars = ax2.bar(['Baseline', 'Cedana'],
                   [baseline_throughput * 1000, cedana_throughput * 1000],  # Convert to jobs/1000s for readability
                   color=[baseline_completed_color, cedana_color],
                   width=0.6, edgecolor='black', linewidth=2.5)

    # Value labels
    for i, bar in enumerate(bars):
        height = bar.get_height()
        throughput_val = baseline_throughput if i == 0 else cedana_throughput
        ax2.text(bar.get_x() + bar.get_width()/2., height * 1.05,
                f'{throughput_val:.6f}\njobs/sec',
                ha='center', va='bottom', fontsize=10, fontweight='bold')

    ax2.set_ylabel('Throughput (jobs/1000s)', fontsize=12, fontweight='bold')
    ax2.set_title('Job Throughput', fontsize=14, fontweight='bold', pad=12)
    ax2.grid(True, axis='y', alpha=0.3)

    # ============================================================================
    # 3. Completion Rate Percentage
    # ============================================================================
    ax3 = fig.add_subplot(gs[0, 2])

    completion_rates = [
        baseline['summary']['completion_rate'] * 100,
        cedana['summary']['completion_rate'] * 100
    ]

    bars = ax3.bar(['Baseline', 'Cedana'], completion_rates,
                   color=[baseline_completed_color, cedana_color],
                   width=0.6, edgecolor='black', linewidth=2.5)

    # Value labels
    for i, bar in enumerate(bars):
        height = bar.get_height()
        ax3.text(bar.get_x() + bar.get_width()/2., height + 3,
                f'{height:.0f}%',
                ha='center', va='bottom', fontsize=13, fontweight='bold')

    ax3.set_ylabel('Completion Rate (%)', fontsize=12, fontweight='bold')
    ax3.set_title('Success Rate', fontsize=14, fontweight='bold', pad=12)
    ax3.set_ylim(0, 110)
    ax3.axhline(y=100, color='gray', linestyle='--', alpha=0.5, linewidth=1.5)
    ax3.grid(True, axis='y', alpha=0.3)

    # ============================================================================
    # 4. Timeline - All Jobs
    # ============================================================================
    ax4 = fig.add_subplot(gs[1, :])

    baseline_jobs = baseline['jobs']
    cedana_jobs = cedana['jobs']

    # Plot baseline jobs
    for job in baseline_jobs:
        job_id = job['job_id']
        y_pos = job_id - 0.2

        # Job execution bar
        if job['status'] == 'completed':
            color = baseline_completed_color
            alpha = 0.7
        else:
            color = failed_color
            alpha = 0.6

        duration = job['completion_time'] - job['start_time']
        ax4.barh(y_pos, duration, left=job['start_time'], height=0.35,
                color=color, alpha=alpha, edgecolor='black', linewidth=1)

        # Mark preemption point
        ax4.scatter(job['preemption_time'], y_pos, s=150, marker='x',
                   color=preempt_color, linewidths=3, zorder=5)

    # Plot cedana jobs
    for job in cedana_jobs:
        job_id = job['job_id']
        y_pos = job_id + 0.2

        # Job execution bar
        color = cedana_color
        alpha = 0.7

        duration = job['completion_time'] - job['start_time']
        ax4.barh(y_pos, duration, left=job['start_time'], height=0.35,
                color=color, alpha=alpha, edgecolor='black', linewidth=1)

        # Mark checkpoint point
        ax4.scatter(job['preemption_time'], y_pos, s=120, marker='o',
                   color='#3498db', edgecolors='black', linewidths=1.5, zorder=5)

    ax4.set_xlabel('Time (seconds)', fontsize=12, fontweight='bold')
    ax4.set_ylabel('Job ID', fontsize=12, fontweight='bold')
    ax4.set_title('Timeline: Job Execution with Preemption Events', fontsize=14, fontweight='bold', pad=12)
    ax4.set_yticks(range(1, config['num_jobs'] + 1))
    ax4.grid(True, axis='x', alpha=0.3)
    ax4.set_xlim(0, max(baseline['total_wall_time_sec'], cedana['total_wall_time_sec']) * 1.05)

    # Legend
    legend_elements = [
        mpatches.Patch(facecolor=baseline_completed_color, alpha=0.7, label='Baseline (completed)', edgecolor='black'),
        mpatches.Patch(facecolor=failed_color, alpha=0.6, label='Baseline (failed)', edgecolor='black'),
        mpatches.Patch(facecolor=cedana_color, alpha=0.7, label='Cedana (completed)', edgecolor='black'),
        plt.Line2D([0], [0], marker='x', color='w', markerfacecolor=preempt_color,
                   markersize=12, markeredgewidth=2.5, label='Preemption (restart 0%)'),
        plt.Line2D([0], [0], marker='o', color='w', markerfacecolor='#3498db',
                   markersize=10, markeredgecolor='black', label='Checkpoint (restore)')
    ]
    ax4.legend(handles=legend_elements, loc='upper right', fontsize=10, framealpha=0.95, edgecolor='black')

    # ============================================================================
    # Main title
    # ============================================================================
    fig.suptitle(
        f'Cedana Throughput Test Results: +{throughput_improvement:.0f}% Throughput Improvement\n' +
        f'{config["num_jobs"]} jobs × {config["job_duration_sec"]//60}min work, {config["wall_clock_limit_sec"]//60}min limit, preempt every {config["preemption_interval_sec"]//60}min',
        fontsize=14, fontweight='bold', y=0.94)

    # Save
    plt.savefig(OUTPUT_FILE, dpi=300, bbox_inches='tight', facecolor='white')
    print(f"\n📊 Generated {len(fig.axes)} visualization panels")

if __name__ == '__main__':
    main()
