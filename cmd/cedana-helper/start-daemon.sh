#!/bin/bash

chroot /host <<"EOT"

./cedana/cedana daemon start&

EOT
