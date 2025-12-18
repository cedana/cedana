package utils

import "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"

func Runtime(pod *k8s.Pod) string {
	if pod.Containerd != nil {
		return "containerd"
	}
	// Add other supported runtimes here as needed

	return "unsupported"
}
