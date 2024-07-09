kubectl delete deployment base-container-0
export count=1
while true; do
  kubectl delete daemonset base-daemonset-$count
  export count=$(($count + 1))
done
