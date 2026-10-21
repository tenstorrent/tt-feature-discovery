# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 Tenstorrent USA, Inc.
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/tt-feature-discovery \
      ./cmd/tt-feature-discovery

# Base sets USER 65532 (nonroot), but the Helm chart overrides the DaemonSet's
# securityContext to runAsUser: 0. Root is required because the process writes
# an NFD feature file into features.d, which the NFD install owns as root:0755 —
# a nonroot uid cannot write there. The nonroot base is kept deliberately: an
# operator who pre-provisions features.d as writable can set securityContext
# back to non-root without rebuilding the image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tt-feature-discovery /usr/local/bin/tt-feature-discovery
ENTRYPOINT ["/usr/local/bin/tt-feature-discovery"]
