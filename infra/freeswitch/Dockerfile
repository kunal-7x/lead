# FreeSWITCH 1.10 with mod_audio_stream for WebSocket audio streaming to voice agent.
#
# mod_audio_stream: streams L16 PCM audio over WebSocket to Python voice agent.
# URL configured via VOICE_AGENT_WS_URL env var (default: ws://voice-agent:8765/audio).
#
# Production: replace VOICE_AGENT_WS_URL in docker-compose / k8s env.
# Real Jio SIP: set JIO_GATEWAY_IP in env; trunk is enabled via ESL on startup.

FROM debian:bookworm-slim AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    git \
    wget \
    curl \
    libssl-dev \
    libcurl4-openssl-dev \
    libpcre3-dev \
    libspeex-dev \
    libspeexdsp-dev \
    libedit-dev \
    libldns-dev \
    liblua5.2-dev \
    libopus-dev \
    pkg-config \
    autoconf \
    automake \
    libtool \
    uuid-dev \
    zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# FreeSWITCH 1.10 source
RUN wget -q https://files.freeswitch.org/releases/freeswitch/freeswitch-1.10.11.-release.tar.gz \
    -O freeswitch.tar.gz \
    && tar xzf freeswitch.tar.gz \
    && mv freeswitch-1.10.11.-release freeswitch

WORKDIR /src/freeswitch

# mod_audio_stream from community (WebSocket audio streaming)
RUN git clone --depth=1 https://github.com/nickolasburr/mod_audio_stream.git \
    src/mod/applications/mod_audio_stream

RUN ./configure --enable-portable-binary \
    --disable-debug \
    --with-openssl \
    && make -j$(nproc) \
    && make install

# Build mod_audio_stream
RUN cd src/mod/applications/mod_audio_stream \
    && /usr/local/freeswitch/bin/freeswitch-config --cflags \
    && make -C . install

# ---- runtime image ----
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libssl3 \
    libcurl4 \
    libedit2 \
    libopus0 \
    uuid-runtime \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/freeswitch /usr/local/freeswitch

COPY conf/ /usr/local/freeswitch/conf/

RUN mkdir -p /tmp/recordings

ENV VOICE_AGENT_WS_URL=ws://voice-agent:8765/audio
ENV JIO_GATEWAY_IP=""
ENV AIRTEL_GATEWAY_HOST=""
ENV ESL_PASSWORD=ClueCon

EXPOSE 5060/udp
EXPOSE 5060/tcp
EXPOSE 5061/tcp
EXPOSE 8021/tcp

ENTRYPOINT ["/usr/local/freeswitch/bin/freeswitch", "-nf", "-nonat"]
