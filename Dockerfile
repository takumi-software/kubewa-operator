# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /workspace

# Copy go module files and download dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code.
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

# Build the operator binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /workspace/manager \
    ./cmd/main.go

# Runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/manager .

# Use nonroot user (uid=65532)
USER 65532:65532

ENTRYPOINT ["/manager"]
