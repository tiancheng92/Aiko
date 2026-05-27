//go:build darwin

package system

/*
#include <mach/mach.h>

static kern_return_t sampleHostCPULoad(host_cpu_load_info_data_t *out) {
	mach_msg_type_number_t count = HOST_CPU_LOAD_INFO_COUNT;
	return host_statistics(mach_host_self(), HOST_CPU_LOAD_INFO,
	                       (host_info_t)out, &count);
}
*/
import "C"
import (
	"runtime"
	"sync"
)

var cpuState struct {
	mu     sync.Mutex
	prev   [4]uint64 // user, system, idle, nice
	inited bool
}

// SampleCPUDelta returns the CPU usage percentage (0–100) since the last call.
//
// Uses host_statistics (Mach) — the same data source Activity Monitor uses.
// Each call samples cumulative CPU ticks across all cores and returns the
// non-idle fraction of the delta from the previous call.
//
// The first call establishes a baseline and returns 0.
func SampleCPUDelta() float64 {
	if runtime.GOOS != "darwin" {
		return 0
	}

	cpuState.mu.Lock()
	defer cpuState.mu.Unlock()

	var load C.host_cpu_load_info_data_t
	if kr := C.sampleHostCPULoad(&load); kr != C.KERN_SUCCESS {
		return 0
	}

	cur := [4]uint64{
		uint64(load.cpu_ticks[C.CPU_STATE_USER]),
		uint64(load.cpu_ticks[C.CPU_STATE_SYSTEM]),
		uint64(load.cpu_ticks[C.CPU_STATE_IDLE]),
		uint64(load.cpu_ticks[C.CPU_STATE_NICE]),
	}

	if !cpuState.inited {
		cpuState.prev = cur
		cpuState.inited = true
		return 0
	}

	prev := cpuState.prev
	cpuState.prev = cur

	var totalDelta, idleDelta uint64
	for i := range 4 {
		delta := cur[i] - prev[i]
		totalDelta += delta
		if i == 2 {
			idleDelta = delta
		}
	}

	if totalDelta == 0 {
		return 0
	}

	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
}
