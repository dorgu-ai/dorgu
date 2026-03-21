package generator

import (
	"fmt"
	"strings"
)

func parseCPUMillis(cpu string) int64 {
	if cpu == "" {
		return 0
	}
	if strings.HasSuffix(cpu, "m") {
		var millis int64
		_, _ = fmt.Sscanf(strings.TrimSuffix(cpu, "m"), "%d", &millis)
		return millis
	}
	var cores float64
	_, _ = fmt.Sscanf(cpu, "%f", &cores)
	return int64(cores * 1000)
}

func parseMemoryBytes(mem string) int64 {
	if mem == "" {
		return 0
	}
	multipliers := map[string]int64{
		"Ki": 1024, "Mi": 1024 * 1024, "Gi": 1024 * 1024 * 1024, "Ti": 1024 * 1024 * 1024 * 1024,
	}
	for suffix, mult := range multipliers {
		if strings.HasSuffix(mem, suffix) {
			var num int64
			_, _ = fmt.Sscanf(strings.TrimSuffix(mem, suffix), "%d", &num)
			return num * mult
		}
	}
	var bytes int64
	_, _ = fmt.Sscanf(mem, "%d", &bytes)
	return bytes
}
