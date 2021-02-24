while true; do

ifdown wlan0

sleepAfterDown=$(shuf -i 5-30 -n 1)

echo "sleeping for ${sleepAfterDown}... (After DOWN)"

sleep $sleepAfterDown

ifup wlan0

sleepAfterUp=$(shuf -i 5-30 -n 1)

echo "sleeping for ${sleepAfterUp}... (After UP)"

sleep $sleepAfterUp

done
