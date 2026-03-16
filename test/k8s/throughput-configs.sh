#!/bin/bash
#
# Throughput Test Configuration Presets
#
# Source this file to use predefined test configurations:
#   source throughput-configs.sh
#   config_quick
#   bats throughput.bats
#

# Reset to defaults
config_default() {
    export THROUGHPUT_NUM_JOBS=10
    export THROUGHPUT_JOB_DURATION=2700      # 45 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=3600  # 60 min
    export THROUGHPUT_PREEMPT_INTERVAL=270   # 4.5 min
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=4200       # 70 min

    echo "✅ Default config: 10 jobs × 45 min work, 60 min limit, preempt every 4.5 min"
}

# Quick test - 5 jobs, 10 min work, 15 min limit
config_quick() {
    export THROUGHPUT_NUM_JOBS=5
    export THROUGHPUT_JOB_DURATION=600       # 10 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=900   # 15 min
    export THROUGHPUT_PREEMPT_INTERVAL=120   # 2 min
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=1200       # 20 min

    echo "✅ Quick test config: 5 jobs × 10 min work, 15 min limit, preempt every 2 min"
}

# Stress test - 20 jobs, 60 min work, 90 min limit
config_stress() {
    export THROUGHPUT_NUM_JOBS=20
    export THROUGHPUT_JOB_DURATION=3600      # 60 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=5400  # 90 min
    export THROUGHPUT_PREEMPT_INTERVAL=180   # 3 min
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=6000       # 100 min

    echo "✅ Stress test config: 20 jobs × 60 min work, 90 min limit, preempt every 3 min"
}

# Multiple preemptions - test recovery from multiple interruptions
config_multi_preempt() {
    export THROUGHPUT_NUM_JOBS=10
    export THROUGHPUT_JOB_DURATION=2700      # 45 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=3600  # 60 min
    export THROUGHPUT_PREEMPT_INTERVAL=270   # 4.5 min
    export THROUGHPUT_PREEMPTIONS_PER_JOB=3  # Preempt each job 3 times
    export THROUGHPUT_JOB_TIMEOUT=4800       # 80 min

    echo "✅ Multi-preempt config: 10 jobs × 45 min work, 3 preemptions per job"
}

# Short jobs - many small jobs to test overhead
config_short_jobs() {
    export THROUGHPUT_NUM_JOBS=15
    export THROUGHPUT_JOB_DURATION=300       # 5 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=480   # 8 min
    export THROUGHPUT_PREEMPT_INTERVAL=60    # 1 min
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=600        # 10 min

    echo "✅ Short jobs config: 15 jobs × 5 min work, 8 min limit, preempt every 1 min"
}

# Long running - test with very long jobs
config_long_running() {
    export THROUGHPUT_NUM_JOBS=5
    export THROUGHPUT_JOB_DURATION=7200      # 120 min (2 hours)
    export THROUGHPUT_WALL_CLOCK_LIMIT=10800 # 180 min (3 hours)
    export THROUGHPUT_PREEMPT_INTERVAL=600   # 10 min
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=12000      # 200 min

    echo "✅ Long running config: 5 jobs × 120 min work, 180 min limit, preempt every 10 min"
}

# Tight margin - wall clock limit very close to job duration
config_tight_margin() {
    export THROUGHPUT_NUM_JOBS=10
    export THROUGHPUT_JOB_DURATION=2700      # 45 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=2820  # 47 min (only 2 min margin!)
    export THROUGHPUT_PREEMPT_INTERVAL=270   # 4.5 min
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=3600       # 60 min

    echo "✅ Tight margin config: 10 jobs × 45 min work, 47 min limit (2 min margin)"
}

# Early preemptions only - all jobs preempted early
config_early_preempt() {
    export THROUGHPUT_NUM_JOBS=10
    export THROUGHPUT_JOB_DURATION=2700      # 45 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=3600  # 60 min
    export THROUGHPUT_PREEMPT_INTERVAL=120   # 2 min (all jobs preempted early)
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=4200       # 70 min

    echo "✅ Early preempt config: 10 jobs × 45 min work, preempt every 2 min (all early)"
}

# Late preemptions only - all jobs preempted late
config_late_preempt() {
    export THROUGHPUT_NUM_JOBS=10
    export THROUGHPUT_JOB_DURATION=2700      # 45 min
    export THROUGHPUT_WALL_CLOCK_LIMIT=3600  # 60 min
    export THROUGHPUT_PREEMPT_INTERVAL=1800  # 30 min (all jobs preempted late)
    export THROUGHPUT_PREEMPTIONS_PER_JOB=1
    export THROUGHPUT_JOB_TIMEOUT=4200       # 70 min

    echo "✅ Late preempt config: 10 jobs × 45 min work, preempt every 30 min (all late)"
}

# List all available configurations
config_list() {
    echo ""
    echo "Available throughput test configurations:"
    echo ""
    echo "  config_default         - Standard test (10 jobs, 45 min work)"
    echo "  config_quick           - Fast test for development (5 jobs, 10 min work)"
    echo "  config_stress          - Stress test (20 jobs, 60 min work)"
    echo "  config_multi_preempt   - Multiple preemptions per job (3x)"
    echo "  config_short_jobs      - Many short jobs (15 jobs, 5 min work)"
    echo "  config_long_running    - Long running jobs (5 jobs, 120 min work)"
    echo "  config_tight_margin    - Tight wall clock margin (47 min limit for 45 min work)"
    echo "  config_early_preempt   - All jobs preempted early"
    echo "  config_late_preempt    - All jobs preempted late"
    echo ""
    echo "Usage:"
    echo "  source throughput-configs.sh"
    echo "  config_quick"
    echo "  bats throughput.bats"
    echo ""
}

# Show current configuration
config_show() {
    echo ""
    echo "Current throughput test configuration:"
    echo "  Jobs:               ${THROUGHPUT_NUM_JOBS:-10}"
    echo "  Job duration:       ${THROUGHPUT_JOB_DURATION:-2700}s ($((${THROUGHPUT_JOB_DURATION:-2700}/60)) min)"
    echo "  Wall clock limit:   ${THROUGHPUT_WALL_CLOCK_LIMIT:-3600}s ($((${THROUGHPUT_WALL_CLOCK_LIMIT:-3600}/60)) min)"
    echo "  Preempt interval:   ${THROUGHPUT_PREEMPT_INTERVAL:-270}s ($((${THROUGHPUT_PREEMPT_INTERVAL:-270}/60)) min)"
    echo "  Preemptions/job:    ${THROUGHPUT_PREEMPTIONS_PER_JOB:-1}"
    echo "  Timeout:            ${THROUGHPUT_JOB_TIMEOUT:-4200}s ($((${THROUGHPUT_JOB_TIMEOUT:-4200}/60)) min)"
    echo ""
}

# If script is sourced, show available configs
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    config_list
fi
