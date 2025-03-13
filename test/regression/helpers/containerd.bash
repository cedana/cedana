
setup_containerd() {
    echo "$(ctr --help)"
    echo "ensure containerd service is enabled"
    run containerd &
    echo "$(ps aux | grep containerd)"
}

cleanup_containerd() {
    run pkill containerd
    echo "$(ps aux | grep containerd)"
}
