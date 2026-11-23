.PHONY: build test linux

build:
	CGO_ENABLED=0 go build -o fan-daemon ./cmd/fan-daemon
	CGO_ENABLED=0 go build -o fanctl ./cmd/fanctl
	CGO_ENABLED=0 go build -o fan-web ./cmd/fan-web

test:
	CGO_ENABLED=0 go test ./...

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fan-daemon-linux ./cmd/fan-daemon
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fanctl-linux ./cmd/fanctl