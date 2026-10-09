package bench

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Hardware describes the host, using unknown for unavailable hardware details.
func Hardware() []string {
	return []string{
		"cpu\t" + cpuModel(),
		"cores\t" + strconv.Itoa(runtime.NumCPU()),
		"memory_gib\t" + memoryGiB(),
		"os\t" + runtime.GOOS + "/" + runtime.GOARCH,
		"go\t" + runtime.Version(),
	}
}

func cpuModel() string {
	switch runtime.GOOS {
	case "darwin":
		return sysctl("machdep.cpu.brand_string")
	case "linux":
		return procValue("/proc/cpuinfo", "model name")
	default:
		return "unknown"
	}
}

func memoryGiB() string {
	var value string
	var scale float64 = 1
	switch runtime.GOOS {
	case "darwin":
		value = sysctl("hw.memsize")
	case "linux":
		fields := strings.Fields(procValue("/proc/meminfo", "MemTotal"))
		if len(fields) != 2 || fields[1] != "kB" {
			return "unknown"
		}
		value = fields[0]
		scale = 1024
	default:
		return "unknown"
	}
	bytes, err := strconv.ParseUint(value, 10, 64)
	if err != nil || bytes == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.1f", float64(bytes)*scale/(1<<30))
}

func sysctl(key string) string {
	output, err := exec.Command("sysctl", "-n", key).Output()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func procValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		name, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(name) == key && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown"
}
