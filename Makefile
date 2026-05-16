.PHONY: proto lint test

proto:
	buf generate

lint:
	buf lint
	go vet ./...
	cd libs/go && go vet ./...

test:
	cd libs/go && go test ./...
	uv run pytest libs/python/evs_common/tests
