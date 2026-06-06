.PHONY: build build-image push-image run restart deploy version release fmt vet lint lint-new test preflight install-hooks cursor-e2e-setup test-cursor-e2e claude-e2e-setup test-claude-e2e

VERSION ?= $(shell git describe --tags --always --dirty)
IMAGE_REPO ?= ghcr.io/dpopsuev/scribe
IMAGE ?= $(IMAGE_REPO):$(VERSION)

version:
	@echo $(VERSION)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=$(VERSION)" -o bin/scribe ./cmd/scribe
	go install -ldflags="-s -w -X main.Version=$(VERSION)" ./cmd/scribe

build-image:
	@test -n "$(VERSION)" || (echo "error: VERSION is not set" && exit 1)
	podman build --build-arg VERSION=$(VERSION) -t $(IMAGE_REPO):$(VERSION) .

push-image:
	@test -n "$(VERSION)" || (echo "error: VERSION is not set" && exit 1)
	podman push $(IMAGE_REPO):$(VERSION)

SCRIBE_DATA ?= $(HOME)/.local/share/scribe

run:
	@SERVICE=$${HOME}/.config/systemd/user/container-scribe.service; \
	if [ -f "$$SERVICE" ]; then \
		sed -i "s|$(IMAGE_REPO):v[^ ]*|$(IMAGE)|g" "$$SERVICE"; \
		systemctl --user daemon-reload; \
		systemctl --user restart container-scribe.service; \
		echo "systemd service restarted with $(IMAGE)"; \
	else \
		podman stop scribe 2>/dev/null || true; \
		podman rm scribe 2>/dev/null || true; \
		podman run -d --name scribe -p 8080:8080 -p 8082:8082 --userns=keep-id \
			-v $(SCRIBE_DATA):/data:Z \
			$(IMAGE) --transport http --addr :8080 --ui --ui-addr :8082; \
	fi
	@sleep 1 && podman logs scribe 2>&1 | tail -3

restart: build-image run

deploy: build-image run

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./... -count=1

test-race:
	go test -race ./... -count=1

test-bench:
	go test ./internal/parchment/ -bench=. -benchmem -count=1

lint:
	golangci-lint run ./...

lint-new:
	golangci-lint run --new-from-rev=HEAD ./...

preflight: fmt vet lint test

install-hooks:
	@echo '#!/bin/sh' > .git/hooks/pre-commit
	@echo 'make lint-new' >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "pre-commit hook installed (runs make lint-new)"

test-stress:
	go test -tags stress -v -timeout 300s -run TestStress .

test-e2e:
	go test -tags e2e -v -timeout 600s -run TestE2E_Deterministic .

test-e2e-llm:
	go test -tags e2e -v -timeout 600s -run TestE2E_LLM .

cursor-e2e-setup:
	cd agent_e2e && npm ci

test-cursor-e2e: cursor-e2e-setup
	RUN_CURSOR_E2E=1 go test -tags cursor_e2e -v -timeout 900s -run TestCursorSDKCanary .

claude-e2e-setup:
	cd agent_e2e && npm ci

test-claude-e2e: claude-e2e-setup
	RUN_CLAUDE_E2E=1 go test -tags claude_e2e -v -timeout 900s -run TestClaudeAgentSDKCanary .

release:
	@test -n "$(V)" || (echo "usage: make release V=v1.2.0" && exit 1)
	sed -i 's|$(IMAGE_REPO):[^ "]*|$(IMAGE_REPO):$(V)|g' README.md
	git add README.md && git commit -m "release: $(V)" || true
	git tag $(V)
	$(MAKE) build-image VERSION=$(V)
	$(MAKE) push-image VERSION=$(V)
	git push origin main --tags
