.PHONY: build test release

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -o dist/iterauthor ./cmd/iterauthor

test:
	go test -race ./... -count=1 -timeout=60s
	go vet ./...

release:
	sh scripts/release.sh
