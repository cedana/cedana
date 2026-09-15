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
		"measure", "benchmark",
		"--size-gb", strconv.FormatFloat(sizeGB, 'f', -1, 64),
		"--samples", strconv.Itoa(samples),
		"--result-file", outputPath,
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
