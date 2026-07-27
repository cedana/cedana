package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cedana/cedana/pkg/measurements"
	"github.com/cedana/cedana/pkg/style"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

func init() {
	measurementsCmd.AddCommand(benchmarkMeasurementsCmd)

	measurementsCmd.Flags().Bool("benchmark-storage", false, "benchmark cached and cold storage throughput")
	measurementsCmd.Flags().Bool("benchmark-memory", false, "benchmark host memory copy throughput")
	measurementsCmd.Flags().String("benchmark-path", ".", "storage path to benchmark")
	measurementsCmd.Flags().Float64("benchmark-size-gb", 1, "benchmark working set size in GB")
	measurementsCmd.Flags().String("storage-mode", "both", "storage benchmark mode: cached, cold, or both")
	measurementsCmd.Flags().Bool("collect-numa", false, "collect NUMA topology rows")
	measurementsCmd.Flags().Bool("benchmark-numa", false, "benchmark NUMA-local memory copy paths")
	measurementsCmd.Flags().Bool("full-numa-matrix", false, "benchmark all NUMA source/target memory paths")

	benchmarkMeasurementsCmd.Flags().String("result-file", "", "write the memory rate to a file")
	benchmarkMeasurementsCmd.Flags().Float64("size-gb", 1, "benchmark working set size in GB")
}

var measurementsCmd = &cobra.Command{
	Use:   "measurements",
	Short: "Collect and print node throughput measurements",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := measurements.Collect(cmd.Context())
		if err != nil {
			return err
		}
		benchmarkStorage, _ := cmd.Flags().GetBool("benchmark-storage")
		benchmarkMemory, _ := cmd.Flags().GetBool("benchmark-memory")
		benchmarkPath, _ := cmd.Flags().GetString("benchmark-path")
		benchmarkSizeGB, _ := cmd.Flags().GetFloat64("benchmark-size-gb")
		storageMode, _ := cmd.Flags().GetString("storage-mode")
		collectNUMA, _ := cmd.Flags().GetBool("collect-numa")
		benchmarkNUMA, _ := cmd.Flags().GetBool("benchmark-numa")
		fullNUMAMatrix, _ := cmd.Flags().GetBool("full-numa-matrix")

		if collectNUMA && !benchmarkNUMA {
			numa, collectErr := measurements.CollectNUMA(cmd.Context())
			if collectErr != nil {
				return collectErr
			}
			report.NUMA = numa
		}
		if benchmarkStorage {
			modes, err := measurements.ParseStorageModes(storageMode)
			if err != nil {
				return err
			}
			report.Storage, err = measurements.BenchmarkStorage(cmd.Context(), benchmarkPath, benchmarkSizeGB, modes)
			if err != nil {
				return err
			}
		}
		if benchmarkMemory {
			report.Memory, err = measurements.BenchmarkMemory(cmd.Context(), benchmarkSizeGB)
			if err != nil {
				return err
			}
		}
		if benchmarkNUMA {
			numa, err := measurements.BenchmarkNUMA(cmd.Context(), benchmarkSizeGB, fullNUMAMatrix)
			if err != nil {
				return err
			}
			report.NUMA = append(report.NUMA, numa...)
		}
		printMeasurementsTable(report)
		return nil
	},
}

var benchmarkMeasurementsCmd = &cobra.Command{
	Use:    "benchmark",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		resultFile, _ := cmd.Flags().GetString("result-file")
		if resultFile == "" {
			return fmt.Errorf("result file is required")
		}
		sizeGB, _ := cmd.Flags().GetFloat64("size-gb")
		rate, err := measurements.BenchmarkMemoryRate(cmd.Context(), sizeGB)
		if err != nil {
			return err
		}
		value := strconv.FormatFloat(rate, 'g', -1, 64)
		return os.WriteFile(resultFile, []byte(value), 0o600)
	},
}

func printMeasurementsTable(report *measurements.Report) {
	tableWriter := table.NewWriter()
	tableWriter.SetStyle(style.TableStyle)
	tableWriter.SetOutputMirror(os.Stdout)
	showFailures := hasMeasurementFailures(report)
	header := table.Row{"section", "name", "metric", "GB/s", "source", "capacity GB", "test GB", "samples"}
	if showFailures {
		header = append(header, "result", "detail")
	}
	tableWriter.AppendHeader(header)

	for _, storage := range report.Storage {
		testSize := formatBenchmarkSize(storage.BenchmarkSizeGB)
		capacity := formatMeasurementGB(storage.CapacityGB)
		appendMeasurementRow(tableWriter, showFailures, "storage", storage.Name, capacity, testSize, storage.BenchmarkRuns, storageMetric("read", storage.Mode), storage.ReadGBPerSec, storage.ReadSource, storage.ReadFailure)
		appendMeasurementRow(tableWriter, showFailures, "storage", storage.Name, capacity, testSize, storage.BenchmarkRuns, storageMetric("write", storage.Mode), storage.WriteGBPerSec, storage.WriteSource, storage.WriteFailure)
	}
	if memory := report.Memory; memory != nil {
		appendMeasurementRow(tableWriter, showFailures, "memory", memoryName(memory), formatMeasurementGB(memory.TotalGB), formatBenchmarkSize(memory.BenchmarkSizeGB), memory.BenchmarkRuns, memoryMetric(memory), memory.CopyGBPerSec, memory.Source, memory.Failure)
	}
	for _, numa := range report.NUMA {
		appendMeasurementRow(tableWriter, showFailures, "memory", numa.Name, formatMeasurementGB(numa.MemoryGB), formatBenchmarkSize(numa.BenchmarkSizeGB), numa.BenchmarkRuns, numaMetric(numa), numa.CopyGBPerSec, numa.Source, numa.Failure)
	}
	for _, gpu := range report.GPUs {
		appendMeasurementRow(tableWriter, showFailures, "gpu", gpuName(gpu), formatMeasurementGB(gpu.MemoryGB), "n/a", 0, "device_memory", gpu.DeviceMemoryGBPerSec, gpu.DeviceMemorySource, gpu.DeviceMemoryFailure)
		appendMeasurementRow(tableWriter, showFailures, "gpu", gpuName(gpu), formatMeasurementGB(gpu.MemoryGB), "n/a", 0, "host_device_link", gpu.HostLinkGBPerSec, gpu.HostLinkSource, gpu.HostLinkFailure)
	}
	for _, failure := range report.Failures {
		failureCopy := failure
		appendMeasurementRow(tableWriter, showFailures, "system", failure.Operation, "n/a", "n/a", 0, "collector", nil, measurements.SourceUnknown, &failureCopy)
	}

	tableWriter.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 2, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 3, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 4, Align: text.AlignRight, AlignHeader: text.AlignRight},
		{Number: 5, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 6, Align: text.AlignRight, AlignHeader: text.AlignRight},
		{Number: 7, Align: text.AlignRight, AlignHeader: text.AlignRight},
		{Number: 8, Align: text.AlignRight, AlignHeader: text.AlignRight},
	})
	tableWriter.Render()
}

func formatMeasurementGB(value float64) string {
	if value <= 0 {
		return "n/a"
	}
	return trimMeasurementFloat(value)
}

func formatBenchmarkSize(value float64) string {
	if value <= 0 {
		return "n/a"
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", value), "0"), ".")
}

func formatMeasurementRate(value *float64) string {
	if value == nil || *value <= 0 {
		return "n/a"
	}
	return trimMeasurementFloat(*value)
}

func trimMeasurementFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", value), "0"), ".")
}
func appendMeasurementRow(writer table.Writer, showFailures bool, section, name, capacity, testSize string, samples int, metric string, rate *float64, source string, failure *measurements.Failure) {
	result := ""
	detail := ""
	if isMeasurementFailure(failure) {
		result = failure.Code
		detail = failure.Message
	}
	row := table.Row{section, name, metric, formatMeasurementRate(rate), sourceStr(source), capacity, testSize, formatSamples(samples)}
	if showFailures {
		row = append(row, result, detail)
	}
	writer.AppendRow(row)
}

func formatSamples(samples int) string {
	if samples <= 0 {
		return "n/a"
	}
	return fmt.Sprint(samples)
}

func storageMetric(direction, mode string) string {
	switch mode {
	case measurements.StorageModeCached:
		return direction + "_cached"
	case measurements.StorageModeCold:
		if direction == "write" {
			return "write_durable"
		}
		return "read_cold"
	default:
		return direction
	}
}

func numaMetric(numa measurements.NUMAMeasurement) string {
	if numa.Kind == "host_memory_copy" && numa.Locality != "" {
		return "host_memory_copy_" + numa.Locality
	}
	if numa.Kind != "" {
		return numa.Kind
	}
	return "numa"
}

func memoryMetric(memory *measurements.MemoryMeasurement) string {
	if memory.Placement == "os_default" || memory.Placement == "unbound" {
		return "host_memory_copy_default"
	}
	if memory.Placement != "" {
		return "host_memory_copy_" + memory.Placement
	}
	return "host_memory_copy"
}

func memoryName(memory *measurements.MemoryMeasurement) string {
	if memory.Hostname != "" {
		return memory.Hostname
	}
	if memory.HostID != "" {
		return memory.HostID
	}
	return "host"
}

func gpuName(gpu measurements.GPUMeasurement) string {
	if gpu.Name != "" {
		return gpu.Name
	}
	if gpu.UUID != "" {
		return gpu.UUID
	}
	return "gpu"
}

func sourceStr(source string) string {
	if source == "" {
		return measurements.SourceUnknown
	}
	return source
}

func isMeasurementFailure(failure *measurements.Failure) bool {
	return failure != nil && failure.Code != measurements.FailureNotMeasured
}

func hasMeasurementFailures(report *measurements.Report) bool {
	if len(report.Failures) > 0 {
		return true
	}
	for _, storage := range report.Storage {
		if isMeasurementFailure(storage.ReadFailure) || isMeasurementFailure(storage.WriteFailure) {
			return true
		}
	}
	if report.Memory != nil && isMeasurementFailure(report.Memory.Failure) {
		return true
	}
	for _, numa := range report.NUMA {
		if isMeasurementFailure(numa.Failure) {
			return true
		}
	}
	for _, gpu := range report.GPUs {
		if isMeasurementFailure(gpu.DeviceMemoryFailure) || isMeasurementFailure(gpu.HostLinkFailure) {
			return true
		}
	}
	return false
}
