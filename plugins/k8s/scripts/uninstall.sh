set -eo pipefail

# Remove all binaries and libraries from the host's filesystem
rm -f /host/usr/local/bin/cedana
rm -f /host/usr/local/lib/libcedana*.so

# Remove config
rm -rf /host/root/.cedana/

# Remove temporary files and logs
rm -rf /var/log/*cedana*
rm -rf /tmp/*cedana*
rm -rf /run/*cedana*
rm -rf /dev/shm/*cedana*
