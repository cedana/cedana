package measurements

import (
	"context"
	"encoding/csv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func collectGPUs(ctx context.Context) ([]GPUMeasurement, error) {
	records, err := queryNvidiaSMI(ctx, "uuid,name,memory.total,driver_version,pci.bus_id")
	if err != nil {
		records, err = queryNvidiaSMI(ctx, "uuid,name,memory.total,driver_version")
	}
	if err != nil {
		return nil, err
	}

	gpus := make([]GPUMeasurement, 0, len(records))
	for _, record := range records {
		if len(record) < 4 {
			continue
		}
		memoryMB, _ := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		gpu := GPUMeasurement{
			UUID: strings.TrimSpace(record[0]), Name: strings.TrimSpace(record[1]),
			MemoryGB: memoryMB / 1000, DriverVersion: strings.TrimSpace(record[3]),
		}
		if len(record) >= 5 {
			gpu.PCIBusID = strings.TrimSpace(record[4])
			gpu.NUMANode = gpuNUMANode(gpu.PCIBusID)
			gpu.HostLinkGBPerSec = gpuHostLinkGBPerSec(gpu.PCIBusID)
		}
		gpu.MemoryBusWidthBits, gpu.MemoryClockMaxMHz, _ = gpuMemoryFacts(gpu.UUID)
		gpu.DeviceMemoryGBPerSec = gpuDeviceMemoryGBPerSec(gpu.MemoryClockMaxMHz, gpu.MemoryBusWidthBits)
		if gpu.DeviceMemoryGBPerSec != nil {
			gpu.DeviceMemorySource = SourceTheoretical
			gpu.DeviceMemoryConfidence = ConfidenceHigh
		} else {
			gpu.DeviceMemoryFailure = &Failure{Code: FailureUnsupported, Operation: "gpu_device_memory", Message: "maximum memory clock or bus width is unavailable from NVML"}
		}
		if gpu.HostLinkGBPerSec != nil {
			gpu.HostLinkSource = SourceTheoretical
			gpu.HostLinkConfidence = ConfidenceHigh
		} else {
			gpu.HostLinkFailure = &Failure{Code: FailureUnsupported, Operation: "gpu_host_device_link", Message: "maximum PCIe link speed or width is unavailable"}
		}
		gpus = append(gpus, gpu)
	}
	return gpus, nil
}

func gpuDeviceMemoryGBPerSec(clockMHz, busWidthBits uint32) *float64 {
	if clockMHz == 0 || busWidthBits == 0 {
		return nil
	}
	const (
		transfersPerClock = 2
		bitsPerByte       = 8
		megaPerGiga       = 1000
	)
	value := float64(clockMHz*transfersPerClock) * float64(busWidthBits) /
		bitsPerByte / megaPerGiga
	return &value
}
func gpuNUMANode(pciBusID string) *int {
	pciBusID = normalizePCIBusID(pciBusID)
	if pciBusID == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join("/sys/bus/pci/devices", pciBusID, "numa_node"))
	if err != nil {
		return nil
	}
	node, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || node < 0 {
		return nil
	}
	return &node
}

func normalizePCIBusID(pciBusID string) string {
	pciBusID = strings.ToLower(strings.TrimSpace(pciBusID))
	parts := strings.SplitN(pciBusID, ":", 2)
	if len(parts) == 2 && len(parts[0]) == 8 {
		pciBusID = parts[0][4:] + ":" + parts[1]
	}
	return pciBusID
}

func queryNvidiaSMI(ctx context.Context, query string) ([][]string, error) {
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu="+query, "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(strings.NewReader(string(output)))
	reader.TrimLeadingSpace = true
	return reader.ReadAll()
}

func gpuHostLinkGBPerSec(pciBusID string) *float64 {
	pciBusID = normalizePCIBusID(pciBusID)
	if pciBusID == "" {
		return nil
	}

	devicePath := filepath.Join("/sys/bus/pci/devices", pciBusID)
	speedData, err := os.ReadFile(filepath.Join(devicePath, "max_link_speed"))
	if err != nil {
		return nil
	}
	widthData, err := os.ReadFile(filepath.Join(devicePath, "max_link_width"))
	if err != nil {
		return nil
	}

	transfersPerSecond, ok := parseFirstFloat(string(speedData))
	if !ok {
		return nil
	}
	width, ok := parseFirstFloat(string(widthData))
	if !ok {
		return nil
	}
	return pcieLinkGBPerSec(transfersPerSecond, width)
}

func pcieLinkGBPerSec(transfersPerSecond, width float64) *float64 {
	if transfersPerSecond <= 0 || width <= 0 {
		return nil
	}

	encodingEfficiency := 128.0 / 130.0
	if transfersPerSecond <= 5 {
		encodingEfficiency = 8.0 / 10.0
	}
	value := transfersPerSecond * width * encodingEfficiency / 8
	return &value
}

func parseFirstFloat(value string) (float64, bool) {
	for _, field := range strings.Fields(strings.TrimSpace(value)) {
		parsed, err := strconv.ParseFloat(strings.Trim(field, "xX"), 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}
