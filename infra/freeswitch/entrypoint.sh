#!/bin/sh
set -e

CONF=/usr/local/freeswitch/conf
: "${ESL_PASSWORD:=ClueCon}"
: "${ESL_APPLY_INBOUND_ACL:=loopback.auto}"
: "${SIP_APPLY_INBOUND_ACL:=}"
: "${VOICE_AGENT_WS_URL:=ws://host.docker.internal:8109/ws/audio}"
: "${JIO_GATEWAY_IP:=}"
: "${AIRTEL_GATEWAY_HOST:=}"

# Expand env placeholders in config XML at container start.
for f in $(grep -rlE '\$\{(ESL_PASSWORD|ESL_APPLY_INBOUND_ACL|SIP_APPLY_INBOUND_ACL|VOICE_AGENT_WS_URL|JIO_GATEWAY_IP|AIRTEL_GATEWAY_HOST)\}' "$CONF" 2>/dev/null); do
    sed -i \
        -e "s|\${ESL_PASSWORD}|${ESL_PASSWORD}|g" \
        -e "s|\${ESL_APPLY_INBOUND_ACL}|${ESL_APPLY_INBOUND_ACL}|g" \
        -e "s|\${SIP_APPLY_INBOUND_ACL}|${SIP_APPLY_INBOUND_ACL}|g" \
        -e "s|\${VOICE_AGENT_WS_URL}|${VOICE_AGENT_WS_URL}|g" \
        -e "s|\${JIO_GATEWAY_IP}|${JIO_GATEWAY_IP}|g" \
        -e "s|\${AIRTEL_GATEWAY_HOST}|${AIRTEL_GATEWAY_HOST}|g" \
        "$f"
done

exec /usr/local/freeswitch/bin/freeswitch -nf -nonat
