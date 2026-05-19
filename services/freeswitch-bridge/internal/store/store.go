package store

import (
	"github.com/lead/services/freeswitch-bridge/internal/esl"
	"github.com/lead/services/freeswitch-bridge/internal/recording"
)

type Store interface {
	esl.SessionStore
	recording.RecordingStore
	Close()
}
