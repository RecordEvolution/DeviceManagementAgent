#!/bin/bash

# --------------------------------------------------------------------------- #

helpmessage=$(cat << 'EOF'
  ...
  Usage:  $ ./monitor-stats.sh --file=<file/path> --timestep=<#seconds> --intervals=<#intervals/steps> --process=<command1,command2,...>
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

# transform commata to spaces
pr=$(echo "${pr}" | sed 's/,/ /g')
echo "${pr}"
for prc in ${pr}
do
	echo "1: ${prc}"
done

# --------------------------------------------------------------------------- #

# construct header
header="#Timestamp,Time,MemTotal[kB],MemFree[kB],MemAvailable[kB]"
for prc in ${pr}
do
	header=$(echo "${header},VSZ(${prc})[kB],RSS(${prc})[kB],CPU(%),MEM(%)")
done
echo "${header}" > "${fl}"

query_total()
{
  ts=$(date +%Y-%m-%d_%H-%M-%S) #-%N)
  tm=$(date +%H:%M:%S)

  memtota=$(cat /proc/meminfo | grep "MemTotal" | awk -F ':' '{print $2}' | awk '{print $1}' | tr -d ' \n')
  memfree=$(cat /proc/meminfo | grep "MemFree" | awk -F ':' '{print $2}' | awk '{print $1}' | tr -d ' \n')
  memavai=$(cat /proc/meminfo | grep "MemAvailable" | awk -F ':' '{print $2}' | awk '{print $1}' | tr -d ' \n')

  echo "${ts},${tm},${memtota},${memfree},${memavai}"
}

query_process()
{
  # get process name
  pr="$1"
  if [[ -z ${pr} ]]; then
	  echo "query_process: missing process name" <&2
	  exit 1
  fi

  # get architecture
  arch=$(uname -m)

  if [[ ${arch} == "armv7l" ]]; then

    # get stats of certain process (exclude grep process itself and process of this script)
    prstats=$(ps -o comm,vsz,rss,args | grep "${pr}" | grep -v "grep" | grep -v "$0" | head -n1)

    # extract Virtual Set Size and Resident Set Size and evtl. convert to kB

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

    prcpu=""
    prmem=""

  elif [[ ${arch} == "x86_64" ]]; then

    prstats=$(ps aux | grep "${pr}" | grep -v "grep" | grep -v "$0" | head -n1)

    prvsz=$(echo "${prstats}" | awk '{print $5}')
    if [[ ! -z "$(echo "${prvsz}" | grep m)" ]]; then
      prvsz=$(echo "${prvsz}" | tr -d "m")
      prvsz=$((prvsz*1000))
    fi
    prrss=$(echo "${prstats}" | awk '{print $6}')
    if [[ ! -z "$(echo "${prrss}" | grep m)" ]]; then
      prrss=$(echo "${prrss}" | tr -d "m")
      prrss=$((prrss*1000))
    fi

    prcpu=$(echo "${prstats}" | awk '{print $3}')
    prmem=$(echo "${prstats}" | awk '{print $4}')

  fi

  if [[ -z ${prstats} ]]; then
    echo "0,0,0,0"
  else
    echo "${prvsz},${prrss},${prcpu},${prmem}"
  fi
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
  data=""
  #data=$(query_stats)
  data=$(echo "${data}$(query_total)")
  for prc in ${pr}
  do
    echo "querying process '${prc}'..."
    dataproc=$(query_process ${prc})
    # echo "${dataproc}"
    # append to line
    data=$(echo "${data},${dataproc}")
  done
  echo "${data}" >> "${fl}"

  # iterate interval counter
  cnt=$((cnt+1))

  # sleep for as long as defined interval
  sleep ${dt}

done


# --------------------------------------------------------------------------- #
