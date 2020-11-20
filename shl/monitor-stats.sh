#!/bin/bash

fl="$1"
if [ -z "${fl}" ]; then
  echo "please provide a file/path to record the stats" >&2
  exit 1
fi

# ask for selection of current stats, i.a. memory, cpu load, ...
query_stats()
{


}

# BusyBox v1.32.0 (2020-11-10 12:33:27 UTC) multi-call binary.
#
# Usage: top [-b] [-n COUNT] [-d SECONDS]
#
# Provide a view of process activity in real time.
# Read the status of all processes from /proc each SECONDS
# and display a screenful of them.
# Keys:
# 	N/M/P/T: sort by pid/mem/cpu/time
# 	R: reverse sort
# 	Q,^C: exit
# Options:
# 	-b	Batch mode
# 	-n N	Exit after N iterations
# 	-d SEC	Delay between updates

# BusyBox v1.32.0 (2020-11-10 12:33:27 UTC) multi-call binary.
#
# Usage: free [-b/k/m/g]
#
# Display the amount of free and used system memory

# BusyBox v1.32.0 (2020-11-10 12:33:27 UTC) multi-call binary.
#
# Usage: ps [-o COL1,COL2=HEADER]
#
# Show list of processes
#
# 	-o COL1,COL2=HEADER	Select columns for display
#
# supported arguments: user,group,comm,args,
#                      pid,ppid,pgid,tty,
#                      vsz (Virtual Set Size),
#                      sid,stat,
#                      rss (Resident Set Size)

# set temporal resolutiong for stat monitoring (in seconds)
dt=2
# number of intervals to monitor
N=200
# count intervals
cnt=0

while [[ ${cnt} -lt ${N} ]]
do

  echo "monitoring stats... interval ${cnt}"

  # query stats
  # query_stats()

  # iterate interval counter
  cnt=$((cnt+1))

  # sleep for as long as defined interval
  sleep ${dt}

done
