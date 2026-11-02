# Third-party licences

Every direct and transitive dependency of every component published from this
repository, with the licence it is distributed under.

This document is generated — do not edit it by hand:

```sh
./hack/generate-sbom.sh                     # refresh sbom/*.json
python3 hack/generate-third-party-licenses.py
```

The source of truth is the CycloneDX SBOM set under [`sbom/`](../sbom). Go
dependencies come from each module's `go.mod`; UI dependencies come from
`ui/package-lock.json`, production scope only, because dev-only packages are
build-time tooling and are not part of any shipped artefact.

## Summary

- **156** distinct third-party dependencies across **8** published components.
- Licences in use: `Apache-2.0` (62), `MIT` (42), `BSD-3-Clause` (26), `Apache-2.0 AND BSD-3-Clause` (15), `Apache-2.0 AND MIT` (4), `Apache-2.0 AND BSD-3-Clause AND MIT` (4), `ISC` (1), `BSD-2-Clause` (1), `Unlicense OR Apache-2.0` (1).
- **No copyleft dependencies.** Every licence above is permissive (Apache-2.0, BSD, MIT, ISC), so redistribution under Apache-2.0 carries no source-disclosure obligation.

## Obligations

| Licence | Obligation when redistributing | How this repository satisfies it |
|---|---|---|
| Apache-2.0 | Retain copyright, licence and NOTICE; state modifications | Dependencies are consumed unmodified as Go modules / npm packages; their licence headers ship inside the module and inside the container image layers. This repository's own licence is [Apache-2.0](../LICENSE). |
| BSD-2-Clause, BSD-3-Clause | Retain copyright notice and disclaimer; do not use the author's name to endorse | Notices ship unmodified with each dependency; no endorsement is claimed. |
| MIT, ISC | Retain copyright notice and permission notice | Notices ship unmodified with each dependency. |
| Unlicense OR Apache-2.0 | Dual-licensed; this project takes the Apache-2.0 branch | Treated as Apache-2.0 above. |

The one vendored artefact in this repository is
`charts/simple-cluster-sync-agent/charts/api-syncagent-0.4.2.tgz`, the upstream
[kcp api-syncagent](https://github.com/kcp-dev/helm-charts) Helm chart,
redistributed unmodified under Apache-2.0 and annotated as such in
[`REUSE.toml`](../REUSE.toml). Its own `LICENSE` travels inside the archive.

## Unmatched licence texts

syft could not match these projects' `LICENSE` files to an SPDX template
verbatim (added preamble, reformatting). Each was resolved by reading the file
in the Go module cache and is recorded in `OVERRIDES` in
`hack/generate-third-party-licenses.py`:

| Dependency | Resolved licence |
|---|---|
| `dario.cat/mergo` v1.0.2 | `BSD-3-Clause` |
| `github.com/grpc-ecosystem/grpc-gateway/v2` v2.30.0 | `UNKNOWN` |
| `github.com/prometheus/client_golang` v1.24.1 | `Apache-2.0` |
| `github.com/prometheus/client_model` v0.6.3 | `Apache-2.0` |
| `github.com/prometheus/common` v0.71.0 | `Apache-2.0` |
| `github.com/prometheus/procfs` v0.22.0 | `Apache-2.0` |
| `k8s.io/kube-openapi` v0.0.0-20260721132016-d427ff9ee9ad | `Apache-2.0` |
| `sigs.k8s.io/kind` v0.33.0 | `Apache-2.0` |
| `sigs.k8s.io/randfill` v1.0.0 | `Apache-2.0` |

## Full inventory

### Apache-2.0 (62)

| Dependency | Version | Used by |
|---|---|---|
| `@luigi-project/client` | `2.32.0` | ui |
| `@sap-theming/theming-base-content` | `11.36.4` | ui |
| `@ui5/webcomponents` | `2.28.1` | ui |
| `@ui5/webcomponents-base` | `2.28.1` | ui |
| `@ui5/webcomponents-fiori` | `2.28.1` | ui |
| `@ui5/webcomponents-icons` | `2.28.1` | ui |
| `@ui5/webcomponents-icons-business-suite` | `2.28.1` | ui |
| `@ui5/webcomponents-icons-tnt` | `2.28.1` | ui |
| `@ui5/webcomponents-localization` | `2.28.1` | ui |
| `@ui5/webcomponents-react` | `2.28.2` | ui |
| `@ui5/webcomponents-react-base` | `2.28.2` | ui |
| `@ui5/webcomponents-theming` | `2.28.1` | ui |
| `cel.dev/expr` | `v0.25.3` | simple-cluster-operator, sshkey-operator |
| `github.com/Masterminds/goutils` | `v1.1.1` | simple-cluster-operator |
| `github.com/go-logr/logr` | `v1.4.4` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-logr/stdr` | `v1.2.2` | simple-cluster-operator, sshkey-operator |
| `github.com/go-logr/zapr` | `v1.3.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/jsonpointer` | `v1.0.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/jsonreference` | `v1.0.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/cmdutils` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/conv` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/fileutils` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/jsonutils` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/loading` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/mangling` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/netutils` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/pools` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/stringutils` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/typeutils` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/swag/yamlutils` | `v0.29.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/google/gnostic-models` | `v0.7.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/gophercloud/gophercloud/v2` | `v2.15.0` | simple-cluster-operator-openstack |
| `github.com/inconshreveable/mousetrap` | `v1.1.0` | simple-cluster-operator, simple-cluster-operator-kind, sshkey-operator |
| `github.com/kcp-dev/apimachinery/v2` | `v2.33.3` | bind-watcher-operator |
| `github.com/kcp-dev/logicalcluster/v3` | `v3.0.5` | bind-watcher-operator |
| `github.com/kcp-dev/multicluster-provider` | `v0.8.0` | bind-watcher-operator |
| `github.com/kcp-dev/sdk` | `v0.33.3` | bind-watcher-operator |
| `github.com/modern-go/concurrent` | `v0.0.0-20180306012644-bacd9c7ef1dd` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/modern-go/reflect2` | `v1.0.3-0.20250322232337-35a7c28c31ee` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/prometheus/client_model` | `v0.6.3` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/prometheus/common` | `v0.71.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/prometheus/procfs` | `v0.22.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/spf13/cobra` | `v1.10.2` | simple-cluster-operator, simple-cluster-operator-kind, sshkey-operator |
| `go.opentelemetry.io/auto/sdk` | `v1.2.1` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/proto/otlp` | `v1.11.0` | simple-cluster-operator, sshkey-operator |
| `gomodules.xyz/jsonpatch/v2` | `v2.5.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `google.golang.org/genproto/googleapis/api` | `v0.0.0-20260911204522-f61a6ca850bd` | simple-cluster-operator, sshkey-operator |
| `google.golang.org/genproto/googleapis/rpc` | `v0.0.0-20260911204522-f61a6ca850bd` | simple-cluster-operator, sshkey-operator |
| `google.golang.org/grpc` | `v1.83.2` | simple-cluster-operator, sshkey-operator |
| `k8s.io/api` | `v0.37.0` | bind-watcher-operator |
| `k8s.io/api` | `v0.37.2` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/apiextensions-apiserver` | `v0.37.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/apiserver` | `v0.37.0` | simple-cluster-operator, sshkey-operator |
| `k8s.io/component-base` | `v0.37.0` | simple-cluster-operator, sshkey-operator |
| `k8s.io/klog/v2` | `v2.140.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/streaming` | `v0.37.2` | simple-cluster-operator, sshkey-operator |
| `sigs.k8s.io/apiserver-network-proxy/konnectivity-client` | `v0.36.0` | simple-cluster-operator, sshkey-operator |
| `sigs.k8s.io/controller-runtime` | `v0.25.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/multicluster-runtime` | `v0.24.1` | bind-watcher-operator |
| `sigs.k8s.io/randfill` | `v1.0.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/structured-merge-diff/v6` | `v6.4.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### MIT (42)

| Dependency | Version | Used by |
|---|---|---|
| `@tanstack/react-virtual` | `3.14.22` | ui |
| `@tanstack/virtual-core` | `3.19.7` | ui |
| `@types/jquery` | `3.5.34` | ui |
| `@types/openui5` | `1.153.0` | ui |
| `@types/qunit` | `2.19.14` | ui |
| `@types/react` | `19.3.0` | ui |
| `@types/react-dom` | `19.3.0` | ui |
| `@types/sizzle` | `2.3.10` | ui |
| `@types/trusted-types` | `2.0.7` | ui |
| `@zxing/library` | `0.21.3` | ui |
| `al.essio.dev/pkg/shellescape` | `v1.6.1` | simple-cluster-operator-kind |
| `clsx` | `2.1.1` | ui |
| `csstype` | `3.2.3` | ui |
| `github.com/BurntSushi/toml` | `v1.6.0` | simple-cluster-operator-kind |
| `github.com/Masterminds/semver/v3` | `v3.5.0` | simple-cluster-operator, sshkey-operator |
| `github.com/Masterminds/sprig/v3` | `v3.3.0` | simple-cluster-operator |
| `github.com/beorn7/perks` | `v1.0.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/blang/semver/v4` | `v4.0.0` | simple-cluster-operator, sshkey-operator |
| `github.com/cenkalti/backoff/v5` | `v5.0.3` | simple-cluster-operator, sshkey-operator |
| `github.com/cespare/xxhash/v2` | `v2.3.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/emicklei/go-restful/v3` | `v3.13.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/felixge/httpsnoop` | `v1.1.0` | simple-cluster-operator, sshkey-operator |
| `github.com/fxamacker/cbor/v2` | `v2.9.4` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-task/slim-sprig/v3` | `v3.0.0` | simple-cluster-operator, sshkey-operator |
| `github.com/huandu/xstrings` | `v1.6.0` | simple-cluster-operator |
| `github.com/json-iterator/go` | `v1.1.12` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/mattn/go-isatty` | `v0.0.24` | simple-cluster-operator-kind |
| `github.com/mitchellh/copystructure` | `v1.2.0` | simple-cluster-operator |
| `github.com/mitchellh/reflectwalk` | `v1.0.2` | simple-cluster-operator |
| `github.com/onsi/ginkgo/v2` | `v2.33.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/onsi/gomega` | `v1.44.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/shopspring/decimal` | `v1.4.0` | simple-cluster-operator |
| `github.com/spf13/cast` | `v1.10.0` | simple-cluster-operator |
| `github.com/x448/float16` | `v0.8.4` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.uber.org/multierr` | `v1.11.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.uber.org/zap` | `v1.28.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `graphql` | `16.14.2` | ui |
| `graphql-sse` | `2.6.3` | ui |
| `react` | `19.3.1` | ui |
| `react-dom` | `19.3.1` | ui |
| `scheduler` | `0.28.0` | ui |
| `ts-custom-error` | `3.3.1` | ui |

### BSD-3-Clause (26)

| Dependency | Version | Used by |
|---|---|---|
| `@lit-labs/ssr-dom-shim` | `1.6.0` | ui |
| `dario.cat/mergo` | `v1.0.2` | simple-cluster-operator |
| `github.com/antlr4-go/antlr/v4` | `v4.13.1` | simple-cluster-operator, sshkey-operator |
| `github.com/evanphx/json-patch/v5` | `v5.9.11` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/fsnotify/fsnotify` | `v1.10.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/google/go-cmp` | `v0.7.0` | bind-watcher-operator, simple-cluster-operator, sshkey-operator |
| `github.com/google/uuid` | `v1.6.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | `v2.30.0` | simple-cluster-operator, sshkey-operator |
| `github.com/munnerz/goautoneg` | `v0.0.0-20191010083416-a7dc8b61c822` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/pmezard/go-difflib` | `v1.0.1-0.20181226105442-5d4384ee4fb2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/spf13/pflag` | `v1.0.10` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/crypto` | `v0.58.0` | simple-cluster-operator, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/exp` | `v0.0.0-20260908205506-85c1c2202aba` | simple-cluster-operator, sshkey-operator |
| `golang.org/x/mod` | `v0.41.0` | simple-cluster-operator, sshkey-operator |
| `golang.org/x/net` | `v0.62.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/oauth2` | `v0.37.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/sync` | `v0.24.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/sys` | `v0.49.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/term` | `v0.47.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/text` | `v0.43.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/time` | `v0.16.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/tools` | `v0.51.0` | simple-cluster-operator, sshkey-operator |
| `google.golang.org/protobuf` | `v1.36.12` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `gopkg.in/evanphx/json-patch.v4` | `v4.13.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `gopkg.in/inf.v0` | `v0.9.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `lit-html` | `2.8.0` | ui |

### Apache-2.0 AND BSD-3-Clause (15)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/google/cel-go` | `v0.31.0` | simple-cluster-operator, sshkey-operator |
| `github.com/google/pprof` | `v0.0.0-20260906184651-6331bc6350fe` | simple-cluster-operator, sshkey-operator |
| `github.com/prometheus/client_golang` | `v1.24.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | `v0.71.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel` | `v1.46.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace` | `v1.46.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | `v1.46.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/metric` | `v1.46.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/sdk` | `v1.46.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/trace` | `v1.46.0` | simple-cluster-operator, sshkey-operator |
| `k8s.io/apimachinery` | `v0.37.0` | bind-watcher-operator |
| `k8s.io/apimachinery` | `v0.37.2` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/utils` | `v0.0.0-20260707023825-cf1189d6abe3` | bind-watcher-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/utils` | `v0.0.0-20261023123148-ca04ba58ecab` | simple-cluster-operator |
| `sigs.k8s.io/json` | `v0.0.0-20260909141634-11ed52e25bc5` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### Apache-2.0 AND BSD-3-Clause AND MIT (4)

| Dependency | Version | Used by |
|---|---|---|
| `k8s.io/client-go` | `v0.37.0` | bind-watcher-operator |
| `k8s.io/client-go` | `v0.37.2` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/kube-openapi` | `v0.0.0-20260721132016-d427ff9ee9ad` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/yaml` | `v1.6.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### Apache-2.0 AND MIT (4)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/pelletier/go-toml` | `v1.9.5` | simple-cluster-operator-kind |
| `go.yaml.in/yaml/v2` | `v2.4.4` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.yaml.in/yaml/v3` | `v3.0.5` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/kind` | `v0.33.0` | simple-cluster-operator-kind |

### BSD-2-Clause (1)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/pkg/errors` | `v0.9.1` | bind-watcher-operator, simple-cluster-operator-kind |

### ISC (1)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/davecgh/go-spew` | `v1.1.2-0.20180830191138-d8f796af33cc` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### Unlicense OR Apache-2.0 (1)

| Dependency | Version | Used by |
|---|---|---|
| `@zxing/text-encoding` | `0.9.0` | ui |
