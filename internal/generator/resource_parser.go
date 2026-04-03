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
		if _, err := fmt.Sscanf(strings.TrimSuffix(cpu, "m"), "%d", &millis); err != nil {
			return 0
		}
		return millis
	}
	var cores float64
	if _, err := fmt.Sscanf(cpu, "%f", &cores); err != nil {
		return 0
	}
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
			if _, err := fmt.Sscanf(strings.TrimSuffix(mem, suffix), "%d", &num); err != nil {
				return 0
			}
			return num * mult
		}
	}
	var bytes int64
	if _, err := fmt.Sscanf(mem, "%d", &bytes); err != nil {
		return 0
	}
	return bytes
}
