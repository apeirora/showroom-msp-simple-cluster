# syntax=docker/dockerfile:1
#
# Build from the repository ROOT so the `replace => ../ubuntu-machines`
# directive in go.mod resolves:
#
#   docker build -f simple-cluster-operator-arsys/Dockerfile \
#     -t arsys-ubuntu-machine-operator:dev .
#
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace
# Sibling module pulled in via a local replace directive.
COPY ubuntu-machines/ ubuntu-machines/
COPY simple-cluster-operator-arsys/ simple-cluster-operator-arsys/
WORKDIR /workspace/simple-cluster-operator-arsys
RUN go mod download || true
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /manager cmd/main.go

# Pure HTTP + Kubernetes client — distroless static is enough.
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
