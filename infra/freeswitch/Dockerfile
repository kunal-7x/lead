# FreeSWITCH 1.10 with mod_audio_stream for WebSocket audio streaming to voice agent.
#
# mod_audio_stream: streams L16 PCM audio over WebSocket to freeswitch-bridge.
# URL configured via VOICE_AGENT_WS_URL env var.
#
# Production: point VOICE_AGENT_WS_URL at freeswitch-bridge /ws/audio.

FROM debian:bookworm-slim AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    git \
    wget \
    curl \
    libssl-dev \
    libcurl4-openssl-dev \
    libjpeg-dev \
    libsqlite3-dev \
    libpcre3-dev \
    libspeex-dev \
    libspeexdsp-dev \
    libspandsp-dev \
    libedit-dev \
    libldns-dev \
    liblua5.2-dev \
    libopus-dev \
    libevent-dev \
    cmake \
    pkg-config \
    autoconf \
    automake \
    libtool \
    uuid-dev \
    zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

RUN git clone --depth=1 --branch v1.13.17 https://github.com/freeswitch/sofia-sip.git /src/sofia-sip \
    && cd /src/sofia-sip \
    && ./bootstrap.sh \
    && ./configure --prefix=/usr/local \
    && make -j$(nproc) \
    && make install

RUN multiarch="$(dpkg-architecture -qDEB_HOST_MULTIARCH)" \
    && printf '%s\n' \
    'prefix=/usr' \
    'exec_prefix=${prefix}' \
    "libdir=\${exec_prefix}/lib/${multiarch}" \
    'includedir=${prefix}/include' \
    '' \
    'Name: spandsp' \
    'Description: spandsp pkg-config shim for Debian libspandsp' \
    'Version: 3.0.0' \
    'Libs: -L${libdir} -lspandsp' \
    "Cflags: -I\${includedir} -I\${includedir}/${multiarch}" \
    > /usr/local/lib/pkgconfig/spandsp.pc

# FreeSWITCH 1.10 source
RUN wget -q https://files.freeswitch.org/releases/freeswitch/freeswitch-1.10.11.-release.tar.gz \
    -O freeswitch.tar.gz \
    && tar xzf freeswitch.tar.gz \
    && mv freeswitch-1.10.11.-release freeswitch

WORKDIR /src/freeswitch

RUN printf '%s\n' \
    applications/mod_commands \
    applications/mod_dptools \
    applications/mod_esf \
    codecs/mod_g711 \
    codecs/mod_l16 \
    endpoints/mod_sofia \
    event_handlers/mod_event_socket \
    loggers/mod_console \
    loggers/mod_logfile \
    > modules.conf
RUN PKG_CONFIG_PATH=/usr/local/lib/pkgconfig ./configure --enable-portable-binary \
    --disable-debug \
    --disable-libvpx \
    --disable-libyuv \
    --with-openssl \
    && make -j$(nproc) \
    && make install

# Build mod_audio_stream with bi-directional playback support.
RUN git clone --depth=1 https://github.com/amigniter/mod_audio_stream.git /src/mod_audio_stream \
    && cd /src/mod_audio_stream \
    && git submodule update --init --recursive \
    && mkdir build \
    && cd build \
    && PKG_CONFIG_PATH=/usr/local/freeswitch/lib/pkgconfig cmake -DCMAKE_BUILD_TYPE=Release .. \
    && make -j$(nproc) \
    && make install

# ---- runtime image ----
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libssl3 \
    libcurl4 \
    libedit2 \
    libjpeg62-turbo \
    libsqlite3-0 \
    libsofia-sip-ua0 \
    libopus0 \
    libspandsp2 \
    libpcre3 \
    libspeex1 \
    libspeexdsp1 \
    libldns3 \
    libevent-2.1-7 \
    liblua5.2-0 \
    ca-certificates \
    uuid-runtime \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/freeswitch /usr/local/freeswitch
COPY --from=builder /usr/local/lib/libsofia* /usr/local/lib/

COPY conf/ /usr/local/freeswitch/conf/
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh && mkdir -p /tmp/recordings

ENV VOICE_AGENT_WS_URL=ws://host.docker.internal:8109/ws/audio
ENV ESL_APPLY_INBOUND_ACL=any_v4.auto
ENV SIP_APPLY_INBOUND_ACL=any_v4.auto
ENV JIO_GATEWAY_IP=""
ENV AIRTEL_GATEWAY_HOST=""
ENV ESL_PASSWORD=ClueCon
ENV LD_LIBRARY_PATH=/usr/local/lib

EXPOSE 5060/udp
EXPOSE 5060/tcp
EXPOSE 5061/tcp
EXPOSE 8021/tcp

ENTRYPOINT ["/entrypoint.sh"]
