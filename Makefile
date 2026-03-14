.PHONY: build build-image push-image version

VERSION ?= $(shell git describe --tags --always --dirty)

version:
	@echo $(VERSION)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o scribe ./cmd/scribe

build-image:
	podman build --build-arg VERSION=$(VERSION) -t ghcr.io/dpopsuev/scribe:$(VERSION) -t ghcr.io/dpopsuev/scribe:latest .

push-image:
	podman push ghcr.io/dpopsuev/scribe:$(VERSION)
	podman push ghcr.io/dpopsuev/scribe:latest
