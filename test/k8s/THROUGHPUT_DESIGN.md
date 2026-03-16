# Throughput Test Design

## Test Scenario

**Goal:** Demonstrate that checkpointing prevents job failures when preemptions occur late in execution.

### Configuration
- **Number of jobs:** 10
- **Job duration:** 45 minutes (2700s) of actual work
- **Wall clock limit:** 1 hour (3600s) per job (enforced via k8s `activeDeadlineSeconds`)
- **Preemption schedule:** Evenly spaced every 270s (4.5 min)
  - Job 1: preempted at 270s (4.5 min)
  - Job 2: preempted at 540s (9 min)
  - Job 3: preempted at 810s (13.5 min)
  - Job 4: preempted at 1080s (18 min)
  - Job 5: preempted at 1350s (22.5 min)
  - Job 6: preempted at 1620s (27 min)
  - Job 7: preempted at 1890s (31.5 min)
  - Job 8: preempted at 2160s (36 min)
  - Job 9: preempted at 2430s (40.5 min)
  - Job 10: preempted at 2700s (45 min)

### Expected Results

#### Baseline (no checkpointing)
Jobs restart from 0% after preemption:
- **Early preemptions** (< 15 min): Jobs complete ✓
  - Job 1 (270s): Restart at 270s, need 2700s, have 3330s left → Complete
  - Job 2 (540s): Restart at 540s, need 2700s, have 3060s left → Complete
  - Job 3 (810s): Restart at 810s, need 2700s, have 2790s left → Complete (barely)
- **Late preemptions** (> 15 min): Jobs FAIL ✗
  - Job 4 (1080s): Restart at 1080s, need 2700s, have 2520s left → FAIL
  - Job 5-10: Similar failures

**Expected:** ~3 jobs complete, ~7 jobs fail

#### Cedana (with checkpointing)
Jobs resume from checkpoint:
- **All preemptions**: Jobs complete ✓
  - Job 1 (270s): Resume at 10% progress, finish in ~2430s
  - Job 10 (2700s): Resume at 100% progress, finish immediately

**Expected:** 10/10 jobs complete

### Data Collection

Output JSON to `/tmp/throughput-test-state/results.json`:

```json
{
  "test_config": {
    "num_jobs": 10,
    "job_duration_sec": 2700,
    "wall_clock_limit_sec": 3600,
    "preemption_interval_sec": 270
  },
  "baseline": {
    "total_wall_time_sec": 0,
    "jobs": [
      {
        "job_id": 1,
        "job_name": "...",
        "start_time": 0,
        "preemption_time": 270,
        "completion_time": 3510,
        "status": "completed",
        "progress_at_preemption": 10.0
      }
    ],
    "summary": {
      "completed": 3,
      "failed": 7,
      "completion_rate": 0.3
    }
  },
  "cedana": {
    "total_wall_time_sec": 0,
    "jobs": [...],
    "summary": {
      "completed": 10,
      "failed": 0,
      "completion_rate": 1.0
    }
  }
}
```

### Visualization

Single Python script (`throughput_viz.py`) that:
1. Reads `results.json`
2. Generates plots:
   - Completion rates (bar chart)
   - Timeline showing job lifecycles
   - Success/failure breakdown
   - Time distribution

### Implementation Notes

- Use `activeDeadlineSeconds: 3600` in job YAML to enforce wall clock limit
- Jobs should self-report progress via logs
- Preemptions executed via `kubectl delete pod` at scheduled times
- For Cedana: checkpoint → delete → restore pattern
- Collect timing data throughout test execution
