#!/bin/sh
set -eu
sed \
  -e "s|\${WEB_PUBLIC_HOST}|${WEB_PUBLIC_HOST}|g" \
  -e "s|\${API_PUBLIC_HOST}|${API_PUBLIC_HOST}|g" \
  /opt/keycloak/config/realm.production.json > /opt/keycloak/data/import/realm.json
exec /opt/keycloak/bin/kc.sh start --import-realm --health-enabled=true --proxy-headers=xforwarded --http-enabled=true
