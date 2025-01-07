#!/usr/bin/env bats
####################
# Uninstall Cedana #
####################
load "./lib/utils.bash"
load "./lib/detik.bash"
DETIK_CLIENT_NAME="kubectl"
pck_version="1.0.1"
DETIK_CLIENT_NAMESPACE="cedana-operator-system"

@test "verify the undeployment" {
	DETIK_CLIENT_NAMESPACE="cedana-operator-system"
	get_cedana_log_check
	sleep 10
	debug "$(helm uninstall cedana -n $DETIK_CLIENT_NAMESPACE)"

	run verify "there is 0 service named 'cedana-cedana-helm-controller-metrics'"
	[ "$status" -eq 0 ]

	run verify "there is 0 daemonset named 'cedana-cedana-helm-helper'"
	[ "$status" -eq 0 ]

	run verify "there is 0 pod named 'cedana'"
	[ "$status" -eq 0 ]
	if ! kubectl apply -f https://raw.githubusercontent.com/cedana/cedana-helm-charts/refs/heads/main/uninstall.yaml; then
		kubectl apply -f https://raw.githubusercontent.com/cedana/cedana-helm-charts/refs/heads/main/uninstall.yaml
		debug "applied new uninstall daemonset"
		sleep 30
		kubectl delete -f https://raw.githubusercontent.com/cedana/cedana-helm-charts/refs/heads/main/uninstall.yaml
		kubectl wait --for=delete daemonset/cedana-helm-helper-uninstaller
		debug "deleted uninstall daemonset pods"
	else
		kubectl delete -f https://raw.githubusercontent.com/cedana/cedana-helm-charts/refs/heads/main/uninstall.yaml
		kubectl wait --for=delete daemonset/cedana-helm-helper-uninstaller
		debug "cleaned existing uninstall daemonset pods"
		kubectl apply -f https://raw.githubusercontent.com/cedana/cedana-helm-charts/refs/heads/main/uninstall.yaml
		debug "applied new uninstall daemonset"
		sleep 30
		kubectl delete -f https://raw.githubusercontent.com/cedana/cedana-helm-charts/refs/heads/main/uninstall.yaml
		kubectl wait --for=delete daemonset/cedana-helm-helper-uninstaller
		debug "deleted uninstall daemonset pods"
	fi
}
