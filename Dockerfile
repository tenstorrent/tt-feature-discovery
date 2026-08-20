# syntax=docker/dockerfile:1.7
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

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tt-feature-discovery /usr/local/bin/tt-feature-discovery
ENTRYPOINT ["/usr/local/bin/tt-feature-discovery"]
