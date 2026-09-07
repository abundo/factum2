#!/usr/bin/with-contenv bash
# Laptop lab: the image sets nginx worker_processes auto (one per CPU) and
# always starts snmpd. Neither is useful here; 20 workers just slow startup.
set -euo pipefail
if [ -f /etc/nginx/nginx.conf ]; then
  sed -i 's/^worker_processes.*/worker_processes 2;/' /etc/nginx/nginx.conf
fi
rm -rf /etc/services.d/snmpd
