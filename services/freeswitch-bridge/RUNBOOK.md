# freeswitch-bridge Runbook

Purpose: bridge FreeSWITCH call control and audio streams into the voice-agent-worker.

Dependencies: FreeSWITCH, telephony-adapter, voice-agent-worker, network audio path.

Paging signals: call answer without audio stream, ESL disconnects, high audio frame loss, bridge restarts.

Common fixes: verify FreeSWITCH container health, confirm mod_audio_stream URL, inspect ESL credentials, fail calls over to mock/demo mode.

Dashboards and logs: active channels, audio stream connections, ESL errors, call setup latency.

Rollback: deploy previous bridge image and disable new call launch if audio path is unstable.
