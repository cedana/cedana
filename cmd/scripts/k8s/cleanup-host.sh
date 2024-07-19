#!/bin/bash

chroot /host /bin/bash <<"EOT"
./cedana/reset.sh

rm -rf /cedana

rm -rf /criu

rm -f /usr/local/bin/cedana
EOT

echo "Clean up completed."
