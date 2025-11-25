package filesystem

import (
	"github.com/shirou/gopsutil/v4/disk"
)

type DiskUsage struct {
	Filesystem string `json:"filesystem"`
	SizeInKb   uint64 `json:"size"`
	Used       uint64 `json:"used"`
	Available  uint64 `json:"available"`
	UsePercent uint8  `json:"use_percent"`
	MountedOn  string `json:"mounted_on"`
}

func AvailableDiskSpace() ([]DiskUsage, error) {
	// Get all disk partitions (including pseudo filesystems on Linux)
	partitions, err := disk.Partitions(false) // false = exclude pseudo filesystems
	if err != nil {
		return []DiskUsage{}, err
	}

	var filesystems []DiskUsage

	// Get usage stats for each partition
	for _, partition := range partitions {
		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			// Skip partitions we can't access
			continue
		}

		fsInfo := DiskUsage{
			Filesystem: partition.Device,
			SizeInKb:   usage.Total / 1024, // Convert bytes to KB
			Used:       usage.Used / 1024,  // Convert bytes to KB
			Available:  usage.Free / 1024,  // Convert bytes to KB
			UsePercent: uint8(usage.UsedPercent),
			MountedOn:  partition.Mountpoint,
		}

		filesystems = append(filesystems, fsInfo)
	}

	return filesystems, nil
}
