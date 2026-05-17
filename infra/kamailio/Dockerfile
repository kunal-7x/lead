# Kamailio 5.8 SIP proxy — routes INVITEs to FreeSWITCH nodes via round-robin dispatcher.
#
# Production: set KAMAILIO_MAX_CPS env (calls per second limit).
# FreeSWITCH nodes registered in dispatcher.list (1 entry per node).
# SIP OPTIONS health check every 10s; unhealthy nodes removed from pool.
# TLS for all internal legs (SIPS).

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    kamailio \
    kamailio-tls-modules \
    kamailio-utils-modules \
    && rm -rf /var/lib/apt/lists/*

COPY kamailio.cfg /etc/kamailio/kamailio.cfg
COPY dispatcher.list /etc/kamailio/dispatcher.list

ENV KAMAILIO_MAX_CPS=50

EXPOSE 5060/udp
EXPOSE 5060/tcp
EXPOSE 5061/tcp

CMD ["kamailio", "-f", "/etc/kamailio/kamailio.cfg", "-DD", "-E"]
