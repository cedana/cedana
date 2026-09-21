package measurements

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	numaBenchmarkChildEnv   = "CEDANA_NUMA_BENCHMARK"
	numaBenchmarkSizeEnv    = "CEDANA_NUMA_BENCHMARK_SIZE_GB"
	numaBenchmarkSamplesEnv = "CEDANA_NUMA_BENCHMARK_SAMPLES"
	numaBenchmarkOutputEnv  = "CEDANA_NUMA_BENCHMARK_OUTPUT"
	numaCalibrationSizeGB   = 0.1
	numaCalibrationSamples  = 3
)

type NUMACalibrator struct {
	limits map[string]int64
}

// NewNUMACalibrator measures the host NUMA copy matrix for profile limits.
func NewNUMACalibrator(ctx context.Context) (*NUMACalibrator, error) {
	measurements, err := BenchmarkNUMA(ctx, numaCalibrationSizeGB, numaCalibrationSamples)
	if err != nil {
		return nil, err
	}

	calibrator := &NUMACalibrator{limits: make(map[string]int64)}
	for _, measurement := range measurements {
		if measurement.CopyGBPerSec == nil || *measurement.CopyGBPerSec <= 0 {
			continue
		}
		calibrator.limits[measurement.Name] = int64(*measurement.CopyGBPerSec * 1_000_000_000)
	}
	return calibrator, nil
}

// LimitForPID returns a measured NUMA copy limit when the process placement is unambiguous.
func (c *NUMACalibrator) LimitForPID(pid uint32) (int64, string, bool) {
	if c == nil || len(c.limits) == 0 || pid == 0 {
		return 0, "", false
	}

	cpuNode, memoryNode, ok := numaPlacementForPID(pid)
	if !ok {
		return 0, "", false
	}
	resource := numaPathName(cpuNode, memoryNode)
	limit, ok := c.limits[resource]
	return limit, resource, ok
}

// RunNUMABenchmarkChild handles the bound child process started by BenchmarkNUMA.
// It is intentionally not a user-facing Cedana command.
func RunNUMABenchmarkChild(ctx context.Context) (bool, error) {
	if os.Getenv(numaBenchmarkChildEnv) == "" {
		return false, nil
	}

	sizeGB, err := strconv.ParseFloat(os.Getenv(numaBenchmarkSizeEnv), 64)
	if err != nil {
		return true, fmt.Errorf("parse NUMA benchmark size: %w", err)
	}
	samples, err := strconv.Atoi(os.Getenv(numaBenchmarkSamplesEnv))
	if err != nil {
		return true, fmt.Errorf("parse NUMA benchmark samples: %w", err)
	}
	rate, err := BenchmarkMemoryRate(ctx, sizeGB, samples)
	if err != nil {
		return true, err
	}
	if err := os.WriteFile(os.Getenv(numaBenchmarkOutputEnv), []byte(strconv.FormatFloat(rate, 'f', -1, 64)), 0o600); err != nil {
		return true, err
	}
	return true, nil
}

func BenchmarkNUMA(ctx context.Context, sizeGB float64, samples int) ([]NUMAMeasurement, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}
	if err := validateBenchmarkSamples(samples); err != nil {
		return nil, err
	}
	nodes, err := discoverNUMANodes(numaSysfsPath)
	if err != nil {
		return nil, err
	}
	if len(nodes) < 2 {
		return nil, nil
	}
	if sizeGB <= 0 {
		sizeGB = 1
	}
	numactl, err := exec.LookPath("numactl")
	if err != nil {
		return []NUMAMeasurement{{
			Name: "numa", Kind: "host_memory_copy", Source: SourceUnknown,
			Failure: &Failure{
				Code:      FailureUnavailable,
				Operation: "numa_memory_copy",
				Message:   "numactl is not installed; NUMA benchmarks were not run",
			},
		}}, nil
	}

	result := make([]NUMAMeasurement, 0, len(nodes)*len(nodes))
	for _, source := range nodes {
		for _, target := range nodes {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			measurement := NUMAMeasurement{
				Name: numaPathName(source.ID, target.ID), Kind: "host_memory_copy",
				Locality: numaLocality(source.ID, target.ID), MemoryGB: target.MemoryGB,
				BenchmarkSizeGB: sizeGB, BenchmarkRuns: samples,
				Source: SourceUnknown,
			}
			rate, benchmarkErr := benchmarkNUMAMemoryAccess(ctx, numactl, source.ID, target.ID, sizeGB, samples)
			if benchmarkErr != nil {
				measurement.Failure = measurementFailure("numa_memory_copy", benchmarkErr)
			} else {
				measurement.CopyGBPerSec = &rate
				measurement.Source = SourceMeasured
			}
			result = append(result, measurement)
		}
	}
	return result, nil
}

type numaNode struct {
	ID       int
	MemoryGB float64
}

func discoverNUMANodes(root string) ([]numaNode, error) {
	matches, err := filepath.Glob(filepath.Join(root, "node*"))
	if err != nil {
		return nil, err
	}
	nodes := make([]numaNode, 0, len(matches))
	for _, match := range matches {
		id, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(match), "node"))
		if err != nil {
			continue
		}
		node := numaNode{ID: id}
		if meminfo, readErr := os.ReadFile(filepath.Join(match, "meminfo")); readErr == nil {
			node.MemoryGB = parseNUMAMemTotalGB(string(meminfo))
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, nil
}

func benchmarkNUMAMemoryAccess(ctx context.Context, numactl string, cpuNode, memoryNode int, sizeGB float64, samples int) (float64, error) {
	outputFile, err := os.CreateTemp("", "cedana-numa-benchmark-*")
	if err != nil {
		return 0, err
	}
	outputPath := outputFile.Name()
	if err := outputFile.Close(); err != nil {
		os.Remove(outputPath)
		return 0, err
	}
	defer os.Remove(outputPath)

	cmd := exec.CommandContext(ctx,
		numactl,
		"--cpunodebind", strconv.Itoa(cpuNode),
		"--membind", strconv.Itoa(memoryNode),
		os.Args[0],
	)
	cmd.Env = append(os.Environ(),
		numaBenchmarkChildEnv+"=1",
		numaBenchmarkSizeEnv+"="+strconv.FormatFloat(sizeGB, 'f', -1, 64),
		numaBenchmarkSamplesEnv+"="+strconv.Itoa(samples),
		numaBenchmarkOutputEnv+"="+outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return 0, fmt.Errorf("%w: %s", errNUMABinding, detail)
	}
	output, err = os.ReadFile(outputPath)
	if err != nil {
		return 0, err
	}
	rate, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", errInvalidOutput, err)
	}
	return rate, nil
}

func parseNUMAMemTotalGB(meminfo string) float64 {
	for _, line := range strings.Split(meminfo, "\n") {
		if !strings.Contains(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		kb, err := strconv.ParseUint(fields[len(fields)-2], 10, 64)
		if err == nil {
			return float64(kb*1024) / 1_000_000_000
		}
	}
	return 0
}

func numaPathName(cpuNode, memoryNode int) string {
	if cpuNode == memoryNode {
		return fmt.Sprintf("numa%d", cpuNode)
	}
	return fmt.Sprintf("numa%d->numa%d", cpuNode, memoryNode)
}

func numaLocality(cpuNode, memoryNode int) string {
	if cpuNode == memoryNode {
		return "local"
	}
	return "remote"
}

func numaPlacementForPID(pid uint32) (int, int, bool) {
	if runtime.GOOS != "linux" {
		return 0, 0, false
	}

	status, err := os.ReadFile(filepath.Join("/proc", strconv.FormatUint(uint64(pid), 10), "status"))
	if err != nil {
		return 0, 0, false
	}
	return numaPlacementFromStatus(string(status), numaNodeForCPU)
}

func numaPlacementFromStatus(status string, nodeForCPU func(int) (int, bool)) (int, int, bool) {
	cpus, ok := statusNUMAList(status, "Cpus_allowed_list")
	if !ok {
		return 0, 0, false
	}
	memoryNodes, ok := statusNUMAList(status, "Mems_allowed_list")
	if !ok || len(memoryNodes) != 1 {
		return 0, 0, false
	}

	cpuNode, ok := singleNUMANodeForCPUs(cpus, nodeForCPU)
	if !ok {
		return 0, 0, false
	}
	return cpuNode, memoryNodes[0], true
}

func statusNUMAList(status, field string) ([]int, bool) {
	prefix := field + ":"
	for _, line := range strings.Split(status, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		values, err := parseNUMAList(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
		return values, err == nil && len(values) > 0
	}
	return nil, false
}

func parseNUMAList(value string) ([]int, error) {
	if value == "" {
		return nil, fmt.Errorf("empty NUMA list")
	}

	seen := make(map[int]struct{})
	for _, part := range strings.Split(value, ",") {
		bounds := strings.SplitN(part, "-", 2)
		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 0 {
			return nil, fmt.Errorf("invalid NUMA list %q", value)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(bounds[1])
			if err != nil || end < start {
				return nil, fmt.Errorf("invalid NUMA list %q", value)
			}
		}
		for node := start; node <= end; node++ {
			seen[node] = struct{}{}
		}
	}

	result := make([]int, 0, len(seen))
	for node := range seen {
		result = append(result, node)
	}
	sort.Ints(result)
	return result, nil
}

func singleNUMANodeForCPUs(cpus []int, nodeForCPU func(int) (int, bool)) (int, bool) {
	var node int
	for i, cpu := range cpus {
		cpuNode, ok := nodeForCPU(cpu)
		if !ok {
			return 0, false
		}
		if i > 0 && cpuNode != node {
			return 0, false
		}
		node = cpuNode
	}
	return node, len(cpus) > 0
}

func numaNodeForCPU(cpu int) (int, bool) {
	matches, err := filepath.Glob(filepath.Join("/sys/devices/system/cpu", fmt.Sprintf("cpu%d", cpu), "node*"))
	if err != nil || len(matches) != 1 {
		return 0, false
	}
	cpuNode, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(matches[0]), "node"))
	if err != nil || cpuNode < 0 {
		return 0, false
	}
	return cpuNode, true
}
