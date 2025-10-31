# Build the manager binary
FROM golang:1.25-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build
# CGO_ENABLED=0 for static binary compatible with distroless
# -a forces rebuilding of packages
# -ldflags="-s -w" strips debug info for smaller binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -ldflags="-s -w" -o manager cmd/main.go

# Use distroless static-debian12 as minimal runtime image
# Non-root user (uid 65532), no shell, no package manager
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

# Copy the manager binary from builder
COPY --from=builder /workspace/manager .

# Run as non-root user (already set by base image)
USER 65532:65532

# Metadata labels
LABEL org.opencontainers.image.title="Minecraft Operator"
LABEL org.opencontainers.image.description="Kubernetes operator for managing PaperMC Minecraft servers"
LABEL org.opencontainers.image.source="https://github.com/lexfrei/minecraft-operator"
LABEL org.opencontainers.image.licenses="BSD-3-Clause"
LABEL org.opencontainers.image.vendor="Aleksei Sviridkin"
LABEL maintainer="f@lex.la"

ENTRYPOINT ["/manager"]
