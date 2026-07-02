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

# Regenerate the .pb.go files under internal/fabric/{fsdpb,orchestratorpb}
# from proto/. Requires protoc, protoc-gen-go and protoc-gen-go-grpc on
# PATH. Only run this when proto/ changes; the generated files are
# committed and the build does not depend on this target.
PROTO_GEN_STAGE := .protobuild
PROTO_MODULE    := github.com/tenstorrent/tt-feature-discovery
.PHONY: proto-gen
proto-gen:
	rm -rf $(PROTO_GEN_STAGE)
	mkdir -p $(PROTO_GEN_STAGE)
	protoc \
	  --proto_path=proto \
	  --go_out=$(PROTO_GEN_STAGE) \
	  --go_opt=Mfactory_system_descriptor.proto=$(PROTO_MODULE)/internal/fabric/fsdpb \
	  --go_opt=Morchestrator.proto=$(PROTO_MODULE)/internal/fabric/orchestratorpb \
	  --go-grpc_out=$(PROTO_GEN_STAGE) \
	  --go-grpc_opt=Mfactory_system_descriptor.proto=$(PROTO_MODULE)/internal/fabric/fsdpb \
	  --go-grpc_opt=Morchestrator.proto=$(PROTO_MODULE)/internal/fabric/orchestratorpb \
	  proto/factory_system_descriptor.proto proto/orchestrator.proto
	cp $(PROTO_GEN_STAGE)/$(PROTO_MODULE)/internal/fabric/fsdpb/*.pb.go internal/fabric/fsdpb/
	cp $(PROTO_GEN_STAGE)/$(PROTO_MODULE)/internal/fabric/orchestratorpb/*.pb.go internal/fabric/orchestratorpb/
	rm -rf $(PROTO_GEN_STAGE)
