# Throughput Test - Complete Guide

## Overview

This test demonstrates how checkpointing prevents job failures when preemptions occur late in execution, using a wall clock time limit scenario.

**Key Insight:** When jobs are preempted late and must restart from 0%, they cannot complete within their wall clock limit and FAIL. With checkpointing, jobs resume from their checkpoint and complete successfully.

## Test Design

**Default Scenario:**
- 10 concurrent jobs, each with 45 minutes (2700s) of actual work
- 1-hour (3600s) wall clock limit per job (enforced via Kubernetes `activeDeadlineSeconds`)
- Evenly spaced preemptions every 4.5 minutes (270s intervals)

**Expected Results:**
- **Baseline:** Jobs preempted late (>15 min) cannot complete in time → ~7 failures, ~3 completions (30% success rate)
- **Cedana:** All jobs complete because they resume from checkpoint → 10/10 completions (100% success rate)

## Quick Start

### Using Preset Configurations

```bash
# Source the configuration presets
source throughput-configs.sh

# List available configurations
config_list

# Use a preset
config_quick          # Fast 10-min test
bats throughput.bats

# Or stress test
config_stress         # 20 jobs, 60 min each
bats throughput.bats
```

### Custom Configuration

```bash
# Set environment variables
export THROUGHPUT_NUM_JOBS=15
export THROUGHPUT_JOB_DURATION=1800      # 30 min
export THROUGHPUT_WALL_CLOCK_LIMIT=2400  # 40 min
export THROUGHPUT_PREEMPT_INTERVAL=180   # 3 min

# Run test
bats throughput.bats

# Generate visualizations
./throughput_viz.py
```

## Configuration Reference

### Test Structure Parameters

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `THROUGHPUT_NUM_JOBS` | Number of jobs to run | `10` | `20` |
| `THROUGHPUT_JOB_DURATION` | Job work duration (seconds) | `2700` (45 min) | `3600` (60 min) |
| `THROUGHPUT_WALL_CLOCK_LIMIT` | Wall clock limit per job (seconds) | `3600` (60 min) | `5400` (90 min) |
| `THROUGHPUT_PREEMPT_INTERVAL` | Interval between preemptions (seconds) | `270` (4.5 min) | `300` (5 min) |
| `THROUGHPUT_PREEMPTIONS_PER_JOB` | Times to preempt each job | `1` | `3` |

### Test Execution Parameters

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `THROUGHPUT_WORKLOADS` | Workload type | `monte-carlo-pi` | `gpu-workload` |
| `THROUGHPUT_NAMESPACE` | Kubernetes namespace | `throughput-test` | `my-test` |
| `THROUGHPUT_JOB_TIMEOUT` | Max time to wait for all jobs (seconds) | `4200` (70 min) | `6000` (100 min) |
| `THROUGHPUT_CHECKPOINT_TIMEOUT` | Max time for checkpoint (seconds) | `300` (5 min) | `600` (10 min) |
| `THROUGHPUT_SAMPLES_DIR` | Path to cedana-samples repo | auto-detected | `/path/to/samples` |

### Advanced Parameters

| Variable | Description | Default |
|----------|-------------|---------|
| `THROUGHPUT_STATE_DIR` | Where to store results | `/tmp/throughput-test-state` |
| `CEDANA_NAMESPACE` | Cedana system namespace | `cedana-systems` |
| `CEDANA_URL` | Cedana API URL | auto-detected from cluster |
| `CEDANA_AUTH_TOKEN` | Cedana auth token | auto-detected from cluster |

## Preset Configurations

### `config_default`
Standard test configuration
- 10 jobs × 45 min work, 60 min limit
- Preempt every 4.5 min
- Expected: ~30% baseline success, 100% Cedana success

### `config_quick`
Fast test for development
- 5 jobs × 10 min work, 15 min limit
- Preempt every 2 min
- Runs in ~20 minutes

### `config_stress`
Stress test with many jobs
- 20 jobs × 60 min work, 90 min limit
- Preempt every 3 min
- Tests scalability

### `config_multi_preempt`
Multiple preemptions per job
- 10 jobs × 45 min work
- 3 preemptions per job
- Tests checkpoint recovery

### `config_short_jobs`
Many small jobs
- 15 jobs × 5 min work, 8 min limit
- Preempt every 1 min
- Tests overhead

### `config_long_running`
Very long jobs
- 5 jobs × 120 min work, 180 min limit
- Preempt every 10 min
- Tests long-running workloads

### `config_tight_margin`
Minimal time margin
- 10 jobs × 45 min work, 47 min limit (2 min margin!)
- Preempt every 4.5 min
- Tests edge cases

### `config_early_preempt`
All jobs preempted early
- 10 jobs × 45 min work
- Preempt every 2 min (all jobs preempted before 15 min mark)
- Baseline should succeed too

### `config_late_preempt`
All jobs preempted late
- 10 jobs × 45 min work
- Preempt every 30 min (all jobs preempted after 15 min mark)
- Baseline should fail completely

## Usage Examples

### Example 1: Quick Validation Test

```bash
source throughput-configs.sh
config_quick
bats throughput.bats
./throughput_viz.py
```

### Example 2: Custom Test - 30 Minute Jobs

```bash
export THROUGHPUT_NUM_JOBS=8
export THROUGHPUT_JOB_DURATION=1800      # 30 min
export THROUGHPUT_WALL_CLOCK_LIMIT=2700  # 45 min
export THROUGHPUT_PREEMPT_INTERVAL=225   # 3.75 min

bats throughput.bats
./throughput_viz.py
```

### Example 3: Multiple Preemptions

```bash
source throughput-configs.sh
config_multi_preempt
bats throughput.bats
```

### Example 4: Compare Different Scenarios

```bash
# Run quick test
source throughput-configs.sh
config_quick
bats throughput.bats
mv /tmp/throughput-test-state/throughput_results.png results_quick.png

# Run stress test
config_stress
bats throughput.bats
mv /tmp/throughput-test-state/throughput_results.png results_stress.png
```

## Understanding the Results

### JSON Output

Results are saved to `/tmp/throughput-test-state/results.json`:

```json
{
  "test_config": {
    "num_jobs": 10,
    "job_duration_sec": 2700,
    "wall_clock_limit_sec": 3600,
    "preemption_interval_sec": 270,
    "preemptions_per_job": 1
  },
  "baseline": {
    "total_wall_time_sec": 3845,
    "jobs": [...],
    "summary": {
      "total": 10,
      "completed": 3,
      "failed": 7,
      "completion_rate": 0.3
    }
  },
  "cedana": {
    "total_wall_time_sec": 2950,
    "jobs": [...],
    "summary": {
      "total": 10,
      "completed": 10,
      "failed": 0,
      "completion_rate": 1.0
    }
  }
}
```

### Visualization

The visualization script generates a comprehensive report showing:

1. **Completion Rates** - Bar chart of completed vs failed jobs
2. **Total Wall Time** - Time to complete all jobs
3. **Success Rate** - Percentage of jobs completed
4. **Timeline** - Visual representation of job lifecycles
5. **Preemption Distribution** - When jobs were preempted
6. **Per-Job Completion Times** - Individual job performance
7. **Summary Statistics** - Key insights and impact

### Key Metrics

- **Completion Rate:** Percentage of jobs that completed successfully
- **Jobs Saved:** Number of failures prevented by checkpointing
- **Time Saved:** Reduction in total wall time
- **Efficiency Gain:** Percentage improvement in throughput

## Choosing the Right Configuration

### For Development/CI
Use `config_quick`:
- Fast execution (~20 min)
- Quick validation
- Lower resource usage

### For Demos
Use `config_default`:
- Clear differentiation (30% vs 100%)
- Reasonable duration (45 min)
- Balanced job count

### For Performance Testing
Use `config_stress`:
- Many concurrent jobs
- Longer duration
- Tests scalability

### For Edge Cases
Use `config_tight_margin`:
- Tests minimal time margins
- Demonstrates critical scenarios
- Shows checkpoint value clearly

## Calculating Your Own Configuration

### Critical Threshold

The critical threshold is the point where jobs can no longer complete if preempted:

```
critical_threshold = wall_clock_limit - job_duration
```

Jobs preempted **before** this threshold can restart and complete.
Jobs preempted **after** this threshold will fail.

**Example:**
- Job duration: 45 min (2700s)
- Wall clock limit: 60 min (3600s)
- Critical threshold: 15 min (900s)

→ Jobs preempted after 15 min will fail

### Recommended Margins

For clear results:
- **Wall clock limit** should be 1.2-1.5× job duration
- **Preemption interval** should create a mix of early/late preemptions
- **Number of jobs** should be enough to show statistical significance (≥5)

### Example Calculation

Want to test 30-minute jobs:

```bash
JOB_DURATION=1800  # 30 min

# Add 50% margin
WALL_CLOCK_LIMIT=$((JOB_DURATION * 3 / 2))  # 45 min

# Critical threshold
CRITICAL=$((WALL_CLOCK_LIMIT - JOB_DURATION))  # 15 min

# Preempt every 3.75 min for 8 jobs (spreads across critical point)
PREEMPT_INTERVAL=$((JOB_DURATION / 8))  # 225s = 3.75 min
NUM_JOBS=8
```

## Troubleshooting

### No results.json file
- Check that the test ran to completion
- Look in `/tmp/throughput-test-state/` for state files
- Check test logs for errors

### All baseline jobs succeed
- Preemptions are too early (increase `THROUGHPUT_PREEMPT_INTERVAL`)
- Wall clock limit is too generous (decrease `THROUGHPUT_WALL_CLOCK_LIMIT`)
- Job duration is too short (increase `THROUGHPUT_JOB_DURATION`)

### All baseline jobs fail
- Preemptions are too late (decrease `THROUGHPUT_PREEMPT_INTERVAL`)
- Wall clock limit is too tight (increase `THROUGHPUT_WALL_CLOCK_LIMIT`)

### Test takes too long
- Reduce `THROUGHPUT_NUM_JOBS`
- Reduce `THROUGHPUT_JOB_DURATION`
- Use `config_quick` preset

### Visualization errors
- Install required packages: `pip install matplotlib numpy`
- Check JSON format: `cat /tmp/throughput-test-state/results.json | jq`

## Files

- **`throughput.bats`** - Main test script
- **`throughput_viz.py`** - Visualization generator
- **`throughput-configs.sh`** - Preset configurations
- **`THROUGHPUT_DESIGN.md`** - Detailed design document
- **`THROUGHPUT_README.md`** - This file

## Output Files

- `/tmp/throughput-test-state/results.json` - Test results
- `/tmp/throughput-test-state/baseline/jobs.json` - Baseline job data
- `/tmp/throughput-test-state/cedana/jobs.json` - Cedana job data
- `throughput_results.png` - Visualization report
