#!/bin/bash

# Launch containerd in the background
containerd > /dev/null &

$@
