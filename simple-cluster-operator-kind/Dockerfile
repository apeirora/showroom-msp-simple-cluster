# syntax=docker/dockerfile:1
FROM golang:1.24 AS builder
WORKDIR /workspace
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager cmd/main.go

# kind's Go SDK shells out to `docker`, so the runtime image needs a
# docker CLI. Distroless doesn't ship one — use debian-slim.
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends docker.io ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /workspace/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
