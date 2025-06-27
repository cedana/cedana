#!/bin/bash

set -e

chroot /host bash /cedana/scripts/host/systemd-reset.sh
