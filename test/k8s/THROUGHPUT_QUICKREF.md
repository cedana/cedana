# Throughput Test - Quick Reference

## One-Line Commands

```bash
# Default test (45 min jobs)
bats throughput.bats

# Quick test (10 min jobs)
source throughput-configs.sh && config_quick && bats throughput.bats

# Stress test (60 min jobs, 20 jobs)
source throughput-configs.sh && config_stress && bats throughput.bats

# Custom test
THROUGHPUT_NUM_JOBS=5 THROUGHPUT_JOB_DURATION=600 bats throughput.bats

# Generate visualizations
./throughput_viz.py
```

## Configuration Variables (Quick Reference)

```bash
# Core parameters (adjust these first)
export THROUGHPUT_NUM_JOBS=10              # Number of jobs
export THROUGHPUT_JOB_DURATION=2700        # Work time (seconds)
export THROUGHPUT_WALL_CLOCK_LIMIT=3600    # Time limit (seconds)
export THROUGHPUT_PREEMPT_INTERVAL=270     # Preempt every X seconds

# Advanced
export THROUGHPUT_PREEMPTIONS_PER_JOB=1    # Preempt each job N times
export THROUGHPUT_JOB_TIMEOUT=4200         # Max wait time
```

## Preset Configs

| Command | Jobs | Duration | Limit | Runtime |
|---------|------|----------|-------|---------|
| `config_quick` | 5 | 10 min | 15 min | ~20 min |
| `config_default` | 10 | 45 min | 60 min | ~70 min |
| `config_stress` | 20 | 60 min | 90 min | ~100 min |
| `config_short_jobs` | 15 | 5 min | 8 min | ~10 min |
| `config_multi_preempt` | 10 | 45 min | 60 min | ~80 min |

## Critical Threshold Formula

```
critical_threshold = wall_clock_limit - job_duration
```

Jobs preempted **after** this point will FAIL (baseline).

**Example:** 60 min limit - 45 min work = 15 min critical threshold

## Expected Results

### Baseline (no checkpoint)
- Jobs preempted **early** (< critical threshold) → ✅ Complete
- Jobs preempted **late** (> critical threshold) → ❌ Fail

### Cedana (with checkpoint)
- **All jobs** → ✅ Complete (resume from checkpoint)

## Common Test Scenarios

### 1. Development/CI
```bash
source throughput-configs.sh && config_quick && bats throughput.bats
```

### 2. Demo/Presentation
```bash
bats throughput.bats  # Uses defaults: clear 30% vs 100% result
```

### 3. Custom 30-min Jobs
```bash
export THROUGHPUT_NUM_JOBS=8
export THROUGHPUT_JOB_DURATION=1800
export THROUGHPUT_WALL_CLOCK_LIMIT=2700
export THROUGHPUT_PREEMPT_INTERVAL=225
bats throughput.bats
```

### 4. All Jobs Fail (Baseline)
```bash
source throughput-configs.sh && config_late_preempt && bats throughput.bats
```

## Output Files

- `/tmp/throughput-test-state/results.json` - Test data
- `throughput_results.png` - Visualization

## Key Metrics

From `results.json`:
- `baseline.summary.completion_rate` - % jobs completed
- `cedana.summary.completion_rate` - % jobs completed (should be 100%)
- Jobs saved = `cedana.completed - baseline.completed`

## Troubleshooting One-Liners

```bash
# Check test status
cat /tmp/throughput-test-state/results.json | jq '.baseline.summary, .cedana.summary'

# View configuration
config_show

# List all presets
config_list

# Test visualization
./throughput_viz.py
```

## Quick Calculation

Want X-minute jobs:

```bash
JOB_DURATION=$((X * 60))
WALL_CLOCK_LIMIT=$((JOB_DURATION * 3 / 2))  # 50% margin
PREEMPT_INTERVAL=$((JOB_DURATION / 10))     # 10 intervals
NUM_JOBS=10

export THROUGHPUT_NUM_JOBS=$NUM_JOBS
export THROUGHPUT_JOB_DURATION=$JOB_DURATION
export THROUGHPUT_WALL_CLOCK_LIMIT=$WALL_CLOCK_LIMIT
export THROUGHPUT_PREEMPT_INTERVAL=$PREEMPT_INTERVAL
```
