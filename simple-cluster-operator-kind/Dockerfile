# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.2 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o manager cmd/main.go

# kind's Go SDK shells out to `docker`, so the runtime image needs a
# docker CLI. Distroless doesn't ship one — use debian-slim.
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends docker.io ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /workspace/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
