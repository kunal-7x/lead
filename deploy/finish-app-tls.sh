#!/usr/bin/env bash
# Run this once app.famit.in resolves to 139.59.89.18 to finish HTTPS.
set -e
IP=$(getent hosts app.famit.in | awk '{print $1}' | head -1)
if [ "$IP" != "139.59.89.18" ]; then echo "app.famit.in does not resolve to this box yet (got: $IP). Aborting."; exit 1; fi
certbot --nginx -d app.famit.in --non-interactive --agree-tos -m axcrio.inc@gmail.com --redirect
systemctl reload nginx
curl -s -o /dev/null -w 'https://app.famit.in/login -> %{http_code}\n' https://app.famit.in/login
