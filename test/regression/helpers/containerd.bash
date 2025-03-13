
setup_containerd() {
    echo "containerd $(containerd --version)"
    echo "ctr $(ctr --version)"
}

pull_image() {
  # pull image to containerd images library locally
  image="$1"
  ctr images pull "$image"
}

setup_containerd
