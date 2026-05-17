package main

import (
	"log"
	"net/http"
	"os"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/handler"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

func main() {
	addr := os.Getenv("TELEPHONY_ADDR")
	if addr == "" {
		addr = ":8108"
	}

	plivoAuthToken := os.Getenv("PLIVO_AUTH_TOKEN")

	s := store.NewFake()
	pub := outbox.NewFakePublisher()

	adapters := []adapter.Telephony{
		adapter.NewExotel(),
		adapter.NewTwilio(),
		adapter.NewMock(),
	}

	wh := webhook.New(plivoAuthToken, s, pub)
	r := routing.New(s, adapters)
	h := handler.New(wh, r)

	log.Printf("telephony-adapter listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatal(err)
	}
}
