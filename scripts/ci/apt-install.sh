#!/bin/bash

set -e -x

export DEBIAN_FRONTEND=noninteractive

install_retry_counter=0
max_apt_retries=5

# This function loops a couple of times over apt-get, hoping to
# avoid CI errors due to errors during apt-get
# hashsum mismatches, DNS errors and similar things
while true; do
	(( install_retry_counter+=1 ))
	if [ "${install_retry_counter}" -gt "${max_apt_retries}" ]; then
		exit 1
	fi
	apt-get update -y && apt-get install -y --no-install-recommends "$@" && break

	# In case it is a network error let's wait a bit.
	echo "Retrying attempt ${install_retry_counter}"
	sleep "${install_retry_counter}"
done