#!/usr/bin/env bats
###############################################
# Install Cedana and redis-example deployment #
###############################################
load "./lib/utils.bash"
load "./lib/detik.bash"
DETIK_CLIENT_NAME="kubectl"
pck_version="1.0.1"
DETIK_CLIENT_NAMESPACE="cedana-operator-system"

@test "verify a real deployment" {
	reset_debug
	DETIK_CLIENT_NAMESPACE="cedana-operator-system"
	debug "$(git clone https://github.com/cedana/cedana-helm-charts.git)"
	cd cedana-helm-charts
	debug "$(helm upgrade -i cedana ./cedana-helm  --create-namespace -n $DETIK_CLIENT_NAMESPACE --set controllerManager.manager.image.repository=$image_repository --set controllerManager.manager.image.tag=$image_tag --set cedanaConfig.cedanaAuthToken=$cedana_auth_token --set cedanaConfig.cedanaUrl=$cedana_url)"

	CEDANA_HELPER="cedana-cedana-helm-helper"
	CEDANA_MANAGER="cedana-cedana-helm-manager"
	CEDANA_METRICS_SERVICE="cedana-cedana-helm-controller-metrics"

	run verify "there is 1 daemonset named '$CEDANA_HELPER'"
	if [ "$status" -eq 0 ]; then
		debug "Verification succeeded: Daemonset '$CEDANA_HELPER' found."
	else
		debug "Verification failed: Daemonset '$CEDANA_HELPER' not found."
		debug "$(kubectl get daemonset -n $DETIK_CLIENT_NAMESPACE)"
		debug "$(kubectl describe ds -n $DETIK_CLIENT_NAMESPACE)"
	fi

	run try "at most 400 times every 1s to get pods named '$CEDANA_HELPER' and verify that 'status' is 'running'"
	if [ "$status" -eq 0 ]; then
		debug "Pods check succeeded: Found running pods for '$CEDANA_HELPER'."
	else
		debug "Pods check failed: Unable to find running pods for '$CEDANA_HELPER'."
		debug "$(kubectl get pods -n $DETIK_CLIENT_NAMESPACE -o wide)"
		debug "$(kubectl describe pods -n $DETIK_CLIENT_NAMESPACE)"
	fi

	run verify "there is 1 service named '$CEDANA_METRICS_SERVICE'"
	if [ "$status" -eq 0 ]; then
		debug "Verification succeeded: Service '$CEDANA_METRICS_SERVICE' found."
	else
		debug "Verification failed: Service '$CEDANA_METRICS_SERVICE' not found."
		debug "$(kubectl get svc -n $DETIK_CLIENT_NAMESPACE)"
		debug "$(kubectl describe svc -n $DETIK_CLIENT_NAMESPACE)"
	fi

	debug "$(kubectl describe pods -n $DETIK_CLIENT_NAMESPACE | grep -i "image:")"

}
