
setup_containerd() {
    echo "$(ctr --help)"
    echo "ensure containerd service is enabled"
    systemctl enable --now containerd
    echo "$(systemctl status containerd)"
}

setup_containerd
