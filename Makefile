.PHONY: proto lint test security-audit chaos-test dr-test

proto:
	buf generate

lint:
	buf lint
	go vet ./...
	cd libs/go && go vet ./...

test:
	cd libs/go && go test ./...
	uv run pytest libs/python/evs_common/tests

security-audit:
	go test ./tests/security

chaos-test:
	python scripts/chaos_test.py

dr-test:
	python scripts/dr_test.py
