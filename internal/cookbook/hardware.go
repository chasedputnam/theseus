package cookbook

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// HardwareProfile describes the machine's compute resources.
type HardwareProfile struct {
	VRAM    int    `json:"vram_mb"`   // GPU VRAM in MB (0 if no GPU)
	RAM     int    `json:"ram_mb"`    // System RAM in MB
	CPUCores int   `json:"cpu_cores"`
	GPUName string `json:"gpu_name"`
	OS      string `json:"os"`
}

// Detect returns the current machine's hardware profile.
func Detect() *HardwareProfile {
	p := &HardwareProfile{
		CPUCores: runtime.NumCPU(),
		OS:       runtime.GOOS,
	}
	p.RAM = detectRAM()
	p.VRAM, p.GPUName = detectGPU()
	return p
}

func detectRAM() int {
	switch runtime.GOOS {
	case "linux":
		return parseMemInfo()
	case "darwin":
		return parseSysctlMem()
	default:
		return 0
	}
}

func parseMemInfo() int {
	out, err := exec.Command("cat", "/proc/meminfo").Output()
	if err != nil {
		return 0
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.Atoi(fields[1])
				return kb / 1024
			}
		}
	}
	return 0
}

func parseSysctlMem() int {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	bytes, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return int(bytes / 1024 / 1024)
}

func detectGPU() (vramMB int, name string) {
	// Try nvidia-smi first
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			parts := strings.SplitN(lines[0], ",", 2)
			if len(parts) == 2 {
				name = strings.TrimSpace(parts[0])
				vramMB, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
				return
			}
		}
	}
	// Try rocm-smi for AMD
	out, err = exec.Command("rocm-smi", "--showmeminfo", "vram", "--csv").Output()
	if err == nil && len(out) > 0 {
		name = "AMD GPU"
		// Parse VRAM from rocm-smi output
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "VRAM Total Memory") {
				fields := strings.Split(line, ",")
				if len(fields) >= 2 {
					bytes, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
					vramMB = int(bytes / 1024 / 1024)
					return
				}
			}
		}
	}
	return 0, ""
}

// FitScore computes a 0-1 score for how well a model fits the hardware.
func FitScore(profile *HardwareProfile, modelVRAM, modelRAM int) float64 {
	if modelVRAM > 0 {
		if profile.VRAM == 0 {
			return 0 // GPU required but none available
		}
		if modelVRAM > profile.VRAM {
			return 0 // Doesn't fit in VRAM
		}
		return 1.0 - float64(modelVRAM)/float64(profile.VRAM)*0.5
	}
	// CPU-only model
	if modelRAM > profile.RAM {
		return 0
	}
	return 1.0 - float64(modelRAM)/float64(profile.RAM)*0.5
}

// FormatVRAM formats VRAM in human-readable form.
func FormatVRAM(mb int) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.0fGB", float64(mb)/1024)
	}
	return fmt.Sprintf("%dMB", mb)
}
