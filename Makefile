# tt-feature-discovery: per-node DaemonSet that publishes
# tenstorrent.com/* node labels via NFD's local feature-file source.

IMAGE       ?= ghcr.io/tenstorrent/tt-feature-discovery
TAG         ?= dev
PLATFORMS   ?= linux/amd64

.PHONY: build
build:
	go build ./...

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: docker-build
docker-build:
	docker buildx build \
		--platform $(PLATFORMS) \
		--tag $(IMAGE):$(TAG) \
		--load \
		.

.PHONY: docker-push
docker-push:
	docker buildx build \
		--platform $(PLATFORMS) \
		--tag $(IMAGE):$(TAG) \
		--push \
		.

.PHONY: ci
ci: vet test build
