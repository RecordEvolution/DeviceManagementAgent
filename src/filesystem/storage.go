package filesystem

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"
)

// Following structure:
// Filesystem      	     Used Available Use% Mounted on
// /dev/mmcblk0p1   28768292 20146340   7137564  74% /
// none             15838876        0  15838876   0% /dev
// tmpfs            15877728        0  15877728   0% /dev/shm
// tmpfs             3175548   296776   2878772  10% /run
// tmpfs                5120        4      5116   1% /run/lock
// tmpfs            15877728        0  15877728   0% /sys/fs/cgroup
// /dev/mmcblk1p1   60212100 39006652  18117116  69% /opt/reagent/docker-apps
// /dev/mmcblk0p40     64511       75     64437   1% /opt/nvidia/esp

type DiskUsage struct {
	Filesystem string `json:"filesystem"`
	SizeInKb   uint64 `json:"size"`
	Used       uint64 `json:"used"`
	Available  uint64 `json:"available"`
	UsePercent uint8  `json:"use_percent"`
	MountedOn  string `json:"mounted_on"`
}

func AvailableDiskSpace() ([]DiskUsage, error) {
	// Define the command and arguments
	cmd := exec.Command("df")

	// Run the command and capture the output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return []DiskUsage{}, err
	}

	// Use a scanner to read the data line by line
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	// Skip the first line
	scanner.Scan()

	var filesystems []DiskUsage

	// Process each line
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Split the line into fields
		fields := strings.Fields(line)

		// Parse the fields into the struct
		SizeInKb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return []DiskUsage{}, err
		}

		used, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return []DiskUsage{}, err
		}

		available, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			return []DiskUsage{}, err
		}

		usePercent, err := strconv.ParseUint(strings.Split(fields[4], "%")[0], 10, 64)
		if err != nil {
			return []DiskUsage{}, err
		}

		fsInfo := DiskUsage{
			Filesystem: fields[0],
			SizeInKb:   SizeInKb,
			Used:       used,
			Available:  available,
			UsePercent: uint8(usePercent),
			MountedOn:  fields[5],
		}

		// Append the struct to the slice
		filesystems = append(filesystems, fsInfo)
	}

	return filesystems, nil
}
