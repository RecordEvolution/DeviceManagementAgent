#!/bin/bash

# --------------------------------------------------------------------------- #

helpmessage=$(cat << 'EOF'
  ...
  Usage:  $ ./monitor-stats.sh --file=<file/path> --timestep=<#seconds> --intervals=<#intervals/steps> --process=<command>
  ...
EOF
)

# number of args
#echo "$#"
#
# echo "arg1 $1"
# echo "arg2 $2"
# echo "arg3 $3"
# echo "arg4 $4"

# concatetenate all arguments
cliargs="$1 $2 $3 $4"
#echo "${cliargs}"

if [[ "$1" == "--help" ]]; then
  echo -e "${helpmessage}"
  exit 0
fi

# extract file/path
fl=$(echo "${cliargs}" | grep "\-\-file=[^ ]*" -o | awk -F '=' '{print $2}')

if [ -z "${fl}" ]; then
  echo -e "${helpmessage}"
  exit 1
fi

# extract timestep or set default
dt=$(echo "${cliargs}" | grep "\-\-timestep=[^ ]*" -o | awk -F '=' '{print $2}')

if [ -z "${dt}" ]; then
  dt=1
fi

# extract number of intervals or set default
N=$(echo "${cliargs}" | grep "\-\-intervals=[^ ]*" -o | awk -F '=' '{print $2}')

if [ -z "${N}" ]; then
  N=300
fi

# extract command name
pr=$(echo "${cliargs}" | grep "\-\-process=[^ ]*" -o | awk -F '=' '{print $2}')

if [ -z "${pr}" ]; then
  echo -e "${helpmessage}"
  exit 1
fi

echo "file:      ${fl}"
echo "timestep:  ${dt}"
echo "intervals: ${N}"
echo "process:   ${pr}"

# --------------------------------------------------------------------------- #

echo "#Timestamp,Time,MemTotal[kB],MemFree[kB],MemAvailable[kB],VSZ(${pr})[kB],RSS(${pr})[kB]" > "${fl}"

# ask for selection of current stats, i.a. memory, cpu load, ...
query_stats()
{
  ts=$(date +%Y-%m-%d_%H-%M-%S) #-%N)
  tm=$(date +%H:%M:%S)

  memtota=$(cat /proc/meminfo | grep "MemTotal" | awk -F ':' '{print $2}' | awk '{print $1}' | tr -d ' \n')
  memfree=$(cat /proc/meminfo | grep "MemFree" | awk -F ':' '{print $2}' | awk '{print $1}' | tr -d ' \n')
  memavai=$(cat /proc/meminfo | grep "MemAvailable" | awk -F ':' '{print $2}' | awk '{print $1}' | tr -d ' \n')

  # get stats of certain process
  prstats=$(ps -o comm,vsz,rss,args | grep "${pr}" | grep -v "grep" | grep -v "$0" | head -n1)
  prvsz=$(echo "${prstats}" | awk '{print $2}')
  if [[ ! -z "$(echo "${prvsz}" | grep m)" ]]; then
	  prvsz=$(echo "${prvsz}" | tr -d "m")
	  prvsz=$((prvsz*1000))
  fi
  prrss=$(echo "${prstats}" | awk '{print $3}')
  if [[ ! -z "$(echo "${prrss}" | grep m)" ]]; then
	  prrss=$(echo "${prrss}" | tr -d "m")
	  prrss=$((prrss*1000))
  fi

  echo "${ts},${tm},${memtota},${memfree},${memavai},${prvsz},${prrss}"
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


# --------------------------------------------------------------------------- #

# count intervals
cnt=0

while [[ ${cnt} -lt ${N} ]]
do

  echo "monitoring stats... interval ${cnt}"

  # query stats
  data=$(query_stats)
  echo "${data}" >> "${fl}"

  # iterate interval counter
  cnt=$((cnt+1))

  # sleep for as long as defined interval
  sleep ${dt}

done


# --------------------------------------------------------------------------- #
