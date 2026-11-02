# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 Tenstorrent USA, Inc.
# tt-feature-discovery: per-node DaemonSet that publishes
# tenstorrent.com/* node labels via NFD's local feature-file source.

GO_MODULE   := github.com/tenstorrent/tt-feature-discovery
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

# Regenerate the fabric-manager gRPC bindings. Requires protoc plus
# protoc-gen-go / protoc-gen-go-grpc on PATH:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
.PHONY: proto
proto:
	protoc --proto_path=proto \
		--go_out=. --go_opt=module=$(GO_MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(GO_MODULE) \
		proto/orchestrator.proto

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
