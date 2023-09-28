get_tcp_seq() {
  pid=$1
  timestamp=$(date +%s)
  for fd in /proc/$pid/fd/*; do
    inode=$(readlink $fd | awk -F: '/socket/ {print $NF}' | tr -d '[]')
    if [ -n "$inode" ]; then
      awk -v inode="$inode" -v ts="$timestamp" '$10==inode {print ts, "local_seq="$7, "remote_seq="$8}' /proc/net/tcp
    fi
  done
}

while true; do
  get_tcp_seq 70992 >> tcp_seq_data.txt
  sleep 1  # adjust the interval as needed
done
