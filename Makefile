.PHONY: build build-image push-image run restart version release

VERSION ?= $(shell git describe --tags --always --dirty)

version:
	@echo $(VERSION)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o bin/scribe ./cmd/scribe

build-image:
	podman build --build-arg VERSION=$(VERSION) -t quay.io/dpopsuev/scribe:$(VERSION) -t quay.io/dpopsuev/scribe:latest .

push-image:
	podman push quay.io/dpopsuev/scribe:$(VERSION)
	podman push quay.io/dpopsuev/scribe:latest

SCRIBE_DATA ?= $(HOME)/.scribe

run:
	podman rm -f scribe 2>/dev/null || true
	podman run -d --name scribe -p 8080:8080 --userns=keep-id \
		-v $(SCRIBE_DATA):/data:Z \
		quay.io/dpopsuev/scribe:latest
	@sleep 1 && podman logs scribe 2>&1 | tail -3

restart: build-image run

test-e2e:
	go test -tags e2e -v -timeout 600s -run TestE2E_Deterministic .

test-e2e-llm:
	go test -tags e2e -v -timeout 600s -run TestE2E_LLM .

release:
	@test -n "$(V)" || (echo "usage: make release V=v1.2.0" && exit 1)
	sed -i 's|quay.io/dpopsuev/scribe:[^ "]*|quay.io/dpopsuev/scribe:$(V)|g' README.md
	git add README.md && git commit -m "release: $(V)" || true
	git tag $(V)
	$(MAKE) build-image VERSION=$(V)
	git push origin main --tags
