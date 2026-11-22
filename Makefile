.PHONY: build test vet fmt run legacy newsvc router

build:
	go build -o bin/ ./cmd/...

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# all three together, Ctrl+C to stop
run:
	./scripts/run-all.sh

legacy:
	go run ./cmd/legacy

newsvc:
	go run ./cmd/newsvc

router:
	go run ./cmd/router -config router.json -debug
