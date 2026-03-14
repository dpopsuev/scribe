.PHONY: build build-image push-image version

VERSION ?= $(shell git describe --tags --always --dirty)

version:
	@echo $(VERSION)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o scribe ./cmd/scribe

build-image:
	podman build --build-arg VERSION=$(VERSION) -t quay.io/dpopsuev/scribe:$(VERSION) -t quay.io/dpopsuev/scribe:latest .

push-image:
	podman push quay.io/dpopsuev/scribe:$(VERSION)
	podman push quay.io/dpopsuev/scribe:latest
