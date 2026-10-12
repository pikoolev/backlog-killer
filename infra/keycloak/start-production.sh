#!/bin/sh
set -eu
realm=/tmp/realm.json
sed \
  -e "s|\${WEB_PUBLIC_HOST}|${WEB_PUBLIC_HOST}|g" \
  -e "s|\${API_PUBLIC_HOST}|${API_PUBLIC_HOST}|g" \
  /opt/keycloak/config/realm.production.json > "$realm"
# Single-node maintenance startup: update the persisted realm before accepting traffic.
/opt/keycloak/bin/kc.sh import --file "$realm" --override true
exec /opt/keycloak/bin/kc.sh start --health-enabled=true --proxy-headers=xforwarded --http-enabled=true
