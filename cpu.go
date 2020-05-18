package main

import (
	"time"

	"github.com/c9s/goprocinfo/linux"
)

func sampleCpuStats() []linux.CPUStat {
	stats, err := linux.ReadStat("/proc/stat")
	if err != nil {
		panic("unable to read /proc/stat")
	}
	return stats.CPUStats
}

func calculateCpuUsage(curr linux.CPUStat, prev linux.CPUStat) float32 {
	prevIdle := prev.Idle + prev.IOWait
	idle := curr.Idle + curr.IOWait

	prevNonIdle := prev.User + prev.Nice + prev.System + prev.IRQ + prev.SoftIRQ + prev.Steal
	nonIdle := curr.User + curr.Nice + curr.System + curr.IRQ + curr.SoftIRQ + curr.Steal

	prevTotal := prevIdle + prevNonIdle
	total := idle + nonIdle

	totalDelta := total - prevTotal
	idleDelta := idle - prevIdle

	return (float32(totalDelta) - float32(idleDelta)) * 100 / float32(totalDelta)
}

func waitTillAverageCpuUsage(usage float32) {
	currCpuStats := sampleCpuStats()
	for true {
		prevCpuStats := currCpuStats
		time.Sleep(2 * time.Second)
		currCpuStats = sampleCpuStats()

		averageCpuUsage := float32(0.0)
		for cpuNum := 0; cpuNum < len(currCpuStats); cpuNum++ {
			averageCpuUsage += calculateCpuUsage(currCpuStats[cpuNum], prevCpuStats[cpuNum])
		}
		averageCpuUsage /= float32(len(currCpuStats))

		if averageCpuUsage < usage {
			break
		}
	}
}
