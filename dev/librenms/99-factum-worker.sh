#!/usr/bin/with-contenv bash
# s6 service: factum2-worker next to LibreNMS in this container.
set -euo pipefail
mkdir -p /run/factum2-worker /etc/services.d/factum-worker
cat >/etc/services.d/factum-worker/run <<'EOL'
#!/usr/bin/with-contenv bash
mkdir -p /run/factum2-worker
exec /opt/factum2/factum2-worker -f /etc/factum2/factum2-worker.yaml start
EOL
chmod +x /etc/services.d/factum-worker/run
