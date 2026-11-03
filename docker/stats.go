package docker

import (
	"context"
	"net/url"
)

const statsLimit = 1 << 20

// Stats is a single reading of a container's resource use.
type Stats struct {
	// CPUPercent is the share of all CPUs as Docker shows it: 100 per fully used CPU, so it may exceed 100.
	CPUPercent float64
	// MemoryUsed is the memory in use without the reclaimable page cache.
	MemoryUsed  uint64
	MemoryLimit uint64
}

type cpuReading struct {
	CPUUsage struct {
		TotalUsage  uint64   `json:"total_usage"`
		PercpuUsage []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint32 `json:"online_cpus"`
}

type statsAnswer struct {
	CPU    cpuReading `json:"cpu_stats"`
	PreCPU cpuReading `json:"precpu_stats"`
	Memory struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			InactiveFile      *uint64 `json:"inactive_file"`
			TotalInactiveFile *uint64 `json:"total_inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
}

// Stats reads the resource use of a container once. The request asks for two samples, so the reading covers
// the interval between them. A stopped container yields zero values if the Engine answers so.
func (c *Client) Stats(ctx context.Context, ref string) (Stats, error) {
	segment, err := checkRef(ref)
	if err != nil {
		return Stats{}, err
	}
	var answer statsAnswer
	err = c.getJSON(ctx, "stats", "/containers/"+segment+"/stats", url.Values{"stream": {"false"}}, statsLimit, &answer)
	if err != nil {
		return Stats{}, err
	}
	return answer.stats(), nil
}

func (a statsAnswer) stats() Stats {
	s := Stats{MemoryLimit: a.Memory.Limit}
	// Page cache is reclaimable: cgroup v2 reports it as inactive_file, cgroup v1 as total_inactive_file.
	var inactive uint64
	switch {
	case a.Memory.Stats.InactiveFile != nil:
		inactive = *a.Memory.Stats.InactiveFile
	case a.Memory.Stats.TotalInactiveFile != nil:
		inactive = *a.Memory.Stats.TotalInactiveFile
	}
	if a.Memory.Usage > inactive {
		s.MemoryUsed = a.Memory.Usage - inactive
	}
	if a.CPU.CPUUsage.TotalUsage > a.PreCPU.CPUUsage.TotalUsage && a.CPU.SystemCPUUsage > a.PreCPU.SystemCPUUsage {
		cpuDelta := float64(a.CPU.CPUUsage.TotalUsage - a.PreCPU.CPUUsage.TotalUsage)
		systemDelta := float64(a.CPU.SystemCPUUsage - a.PreCPU.SystemCPUUsage)
		online := float64(a.CPU.OnlineCPUs)
		if online == 0 {
			online = float64(len(a.CPU.CPUUsage.PercpuUsage))
		}
		s.CPUPercent = cpuDelta / systemDelta * online * 100
	}
	return s
}
