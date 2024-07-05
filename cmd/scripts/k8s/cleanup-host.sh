#!/bin/bash

chroot /host /bin/bash <<"EOT"
./cedana/reset.sh

rm -rf /cedana

rm -rf /criu
EOT

echo "Clean up completed."
