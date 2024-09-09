#!/usr/bin/env bash
#shellcheck disable=SC2016

cp /usr/local/bin/otelcol-contrib /host/usr/local/bin/otelcol-contrib
cp /usr/local/bin/otelcol-config.yaml /host/usr/local/bin/otelcol-config.yaml

env \
    SIGNOZ_ACCESS_TOKEN="$SIGNOZ_ACCESS_TOKEN" \
  chroot /host /bin/bash -c '
#!/bin/bash
# check for SIGNOZ_ACCESS_TOKEN env var

if [ -z "$SIGNOZ_ACCESS_TOKEN" ]; then
  echo "SIGNOZ_ACCESS_TOKEN unset"
    exit 1
fi

/usr/local/bin/otelcol-contrib --config /usr/local/bin/otelcol-config.yaml > /dev/null 2>&1 &
'
