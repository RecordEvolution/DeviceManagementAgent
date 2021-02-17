#!/bin/bash

# update reagent
update_agent() {

}

# restart reagent
restart_agent() {

}

# check agent process
check_agent() {

}

# list available agents
list_agents() {
  
}

while true
do
	# check for running reagent process
  prcs=$(ps aux | grep "reagent" | grep -v "grep")

  if [ -z ${prcs} ]; then
    # use i-th latest reagent available
    reag=$(ls -t /opt/reagent/ | head -n1)....
  fi

  # check for running reagent and any updates every n seconds
	sleep 10
done
