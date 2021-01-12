
set datafile separator ','

set xdata time
set timefmt "%Y-%m-%d_%H-%M-%S"

set xlabel "time [min:sec]"
set ylabel "memory [MB]"

#set style line 100 lt 1 lc rgb "grey" lw 1.5
#set style line 100 lt 1 lw 2
set grid #ls 100
set ytics 100
#set xtics 1
set xtics rotate

set yrange [-80:2000]

set terminal pngcairo size 1200,800 #enhanced font 'Segoe UI,10'
set output 'device_management_agent.png'

plot 'device_management_agent.log' using 0:($3/1000) w l lw 2 title 'mem total', \
                                '' using 0:($4/1000) w l lw 2 title 'mem free', \
                                '' using 0:($5/1000) w l lw 2 title 'mem avail', \
                                '' using 0:($6/1000) w l lw 2 title 'vsz pyt', \
                                '' using 0:($7/1000) w l lw 2  title 'rss pyt', \
                                '' using 0:($8/1000) w l lw 2 title 'vsz go', \
                                '' using 0:($9/1000) w l lw 2 title 'rss go'
