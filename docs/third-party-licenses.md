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

- **160** distinct third-party dependencies across **8** published components.
- Licences in use: `Apache-2.0` (58), `MIT` (50), `BSD-3-Clause` (28), `Apache-2.0 AND BSD-3-Clause` (13), `Apache-2.0 AND MIT` (5), `ISC` (2), `Apache-2.0 AND BSD-3-Clause AND MIT` (2), `BSD-2-Clause` (1), `Unlicense OR Apache-2.0` (1).
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
| `dario.cat/mergo` v1.0.1 | `BSD-3-Clause` |
| `github.com/prometheus/client_golang` v1.22.0 | `Apache-2.0` |
| `github.com/prometheus/client_model` v0.6.1 | `Apache-2.0` |
| `github.com/prometheus/common` v0.62.0 | `Apache-2.0` |
| `github.com/prometheus/procfs` v0.15.1 | `Apache-2.0` |
| `k8s.io/kube-openapi` v0.0.0-20250710124328-f3f2b991d03b | `Apache-2.0` |
| `sigs.k8s.io/kind` v0.30.0 | `Apache-2.0` |
| `sigs.k8s.io/randfill` v1.0.0 | `Apache-2.0` |

## Full inventory

### Apache-2.0 (58)

| Dependency | Version | Used by |
|---|---|---|
| `@luigi-project/client` | `2.30.0` | ui |
| `@sap-theming/theming-base-content` | `11.35.0` | ui |
| `@ui5/webcomponents` | `2.22.0` | ui |
| `@ui5/webcomponents-base` | `2.22.0` | ui |
| `@ui5/webcomponents-fiori` | `2.22.0` | ui |
| `@ui5/webcomponents-icons` | `2.22.0` | ui |
| `@ui5/webcomponents-icons-business-suite` | `2.22.0` | ui |
| `@ui5/webcomponents-icons-tnt` | `2.22.0` | ui |
| `@ui5/webcomponents-localization` | `2.22.0` | ui |
| `@ui5/webcomponents-react` | `2.22.2` | ui |
| `@ui5/webcomponents-react-base` | `2.22.2` | ui |
| `@ui5/webcomponents-theming` | `2.22.0` | ui |
| `cel.dev/expr` | `v0.25.1` | bind-watcher-operator, simple-cluster-operator, sshkey-operator |
| `github.com/Masterminds/goutils` | `v1.1.1` | simple-cluster-operator |
| `github.com/go-logr/logr` | `v1.4.3` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-logr/stdr` | `v1.2.2` | simple-cluster-operator, sshkey-operator |
| `github.com/go-logr/zapr` | `v1.3.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/jsonpointer` | `v0.21.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/jsonreference` | `v0.20.2` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-openapi/jsonreference` | `v0.21.0` | bind-watcher-operator |
| `github.com/go-openapi/swag` | `v0.23.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/google/btree` | `v1.1.3` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/google/gnostic-models` | `v0.7.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/gophercloud/gophercloud/v2` | `v2.4.0` | simple-cluster-operator-openstack |
| `github.com/inconshreveable/mousetrap` | `v1.1.0` | simple-cluster-operator, simple-cluster-operator-kind, sshkey-operator |
| `github.com/kcp-dev/apimachinery/v2` | `v2.0.1-0.20250728122101-adbf20db3e51` | bind-watcher-operator |
| `github.com/kcp-dev/kcp/sdk` | `v0.28.1` | bind-watcher-operator |
| `github.com/kcp-dev/logicalcluster/v3` | `v3.0.5` | bind-watcher-operator |
| `github.com/kcp-dev/multicluster-provider` | `v0.2.1` | bind-watcher-operator |
| `github.com/m-szalik/goutils` | `v0.4.0` | bind-watcher-operator |
| `github.com/modern-go/concurrent` | `v0.0.0-20180306012644-bacd9c7ef1dd` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/modern-go/reflect2` | `v1.0.3-0.20250322232337-35a7c28c31ee` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/prometheus/client_model` | `v0.6.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/prometheus/common` | `v0.62.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/prometheus/procfs` | `v0.15.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/spf13/cobra` | `v1.9.1` | simple-cluster-operator, simple-cluster-operator-kind, sshkey-operator |
| `go.opentelemetry.io/auto/sdk` | `v1.2.1` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | `v0.58.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace` | `v1.34.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | `v1.34.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/proto/otlp` | `v1.5.0` | simple-cluster-operator, sshkey-operator |
| `gomodules.xyz/jsonpatch/v2` | `v2.4.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `google.golang.org/genproto/googleapis/api` | `v0.0.0-20260414002931-afd174a4e478` | bind-watcher-operator, simple-cluster-operator, sshkey-operator |
| `google.golang.org/genproto/googleapis/rpc` | `v0.0.0-20260414002931-afd174a4e478` | bind-watcher-operator, simple-cluster-operator, sshkey-operator |
| `google.golang.org/grpc` | `v1.82.1` | simple-cluster-operator, sshkey-operator |
| `k8s.io/api` | `v0.34.0` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/api` | `v0.34.1` | bind-watcher-operator |
| `k8s.io/apiextensions-apiserver` | `v0.34.0` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/apiextensions-apiserver` | `v0.34.1` | bind-watcher-operator |
| `k8s.io/apiserver` | `v0.34.0` | simple-cluster-operator, sshkey-operator |
| `k8s.io/component-base` | `v0.34.0` | simple-cluster-operator, sshkey-operator |
| `k8s.io/klog/v2` | `v2.130.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/apiserver-network-proxy/konnectivity-client` | `v0.31.2` | simple-cluster-operator, sshkey-operator |
| `sigs.k8s.io/controller-runtime` | `v0.22.1` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/controller-runtime` | `v0.22.3` | bind-watcher-operator |
| `sigs.k8s.io/multicluster-runtime` | `v0.21.0-alpha.9` | bind-watcher-operator |
| `sigs.k8s.io/randfill` | `v1.0.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/structured-merge-diff/v6` | `v6.3.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### MIT (50)

| Dependency | Version | Used by |
|---|---|---|
| `@tanstack/react-virtual` | `3.13.25` | ui |
| `@tanstack/virtual-core` | `3.15.0` | ui |
| `@types/jquery` | `3.5.34` | ui |
| `@types/openui5` | `1.148.0` | ui |
| `@types/prop-types` | `15.7.15` | ui |
| `@types/qunit` | `2.19.14` | ui |
| `@types/react` | `18.3.30` | ui |
| `@types/react-dom` | `18.3.7` | ui |
| `@types/sizzle` | `2.3.10` | ui |
| `@types/trusted-types` | `2.0.7` | ui |
| `@zxing/library` | `0.21.3` | ui |
| `al.essio.dev/pkg/shellescape` | `v1.5.1` | simple-cluster-operator-kind |
| `clsx` | `2.1.1` | ui |
| `csstype` | `3.2.3` | ui |
| `github.com/BurntSushi/toml` | `v1.4.0` | simple-cluster-operator-kind |
| `github.com/Masterminds/semver/v3` | `v3.3.0` | simple-cluster-operator |
| `github.com/Masterminds/sprig/v3` | `v3.3.0` | simple-cluster-operator |
| `github.com/beorn7/perks` | `v1.0.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/blang/semver/v4` | `v4.0.0` | simple-cluster-operator, sshkey-operator |
| `github.com/cenkalti/backoff/v4` | `v4.3.0` | simple-cluster-operator, sshkey-operator |
| `github.com/cespare/xxhash/v2` | `v2.3.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/emicklei/go-restful/v3` | `v3.12.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/felixge/httpsnoop` | `v1.0.4` | simple-cluster-operator, sshkey-operator |
| `github.com/fxamacker/cbor/v2` | `v2.9.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/go-task/slim-sprig/v3` | `v3.0.0` | simple-cluster-operator, sshkey-operator |
| `github.com/huandu/xstrings` | `v1.5.0` | simple-cluster-operator |
| `github.com/josharian/intern` | `v1.0.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/json-iterator/go` | `v1.1.12` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/mailru/easyjson` | `v0.7.7` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/mailru/easyjson` | `v0.9.0` | bind-watcher-operator |
| `github.com/mattn/go-isatty` | `v0.0.20` | simple-cluster-operator-kind |
| `github.com/mitchellh/copystructure` | `v1.2.0` | simple-cluster-operator |
| `github.com/mitchellh/reflectwalk` | `v1.0.2` | simple-cluster-operator |
| `github.com/onsi/ginkgo/v2` | `v2.22.0` | simple-cluster-operator, sshkey-operator |
| `github.com/onsi/gomega` | `v1.36.1` | simple-cluster-operator, sshkey-operator |
| `github.com/shopspring/decimal` | `v1.4.0` | simple-cluster-operator |
| `github.com/spf13/cast` | `v1.7.0` | simple-cluster-operator |
| `github.com/stoewer/go-strcase` | `v1.3.0` | simple-cluster-operator, sshkey-operator |
| `github.com/stretchr/testify` | `v1.11.1` | bind-watcher-operator |
| `github.com/x448/float16` | `v0.8.4` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.uber.org/multierr` | `v1.11.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.uber.org/zap` | `v1.27.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `graphql` | `16.14.1` | ui |
| `graphql-sse` | `2.6.0` | ui |
| `js-tokens` | `4.0.0` | ui |
| `loose-envify` | `1.4.0` | ui |
| `react` | `18.3.1` | ui |
| `react-dom` | `18.3.1` | ui |
| `scheduler` | `0.23.2` | ui |
| `ts-custom-error` | `3.3.1` | ui |

### BSD-3-Clause (28)

| Dependency | Version | Used by |
|---|---|---|
| `@lit-labs/ssr-dom-shim` | `1.6.0` | ui |
| `dario.cat/mergo` | `v1.0.1` | simple-cluster-operator |
| `github.com/antlr4-go/antlr/v4` | `v4.13.0` | simple-cluster-operator, sshkey-operator |
| `github.com/evanphx/json-patch/v5` | `v5.9.11` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/fsnotify/fsnotify` | `v1.9.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/gogo/protobuf` | `v1.3.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/google/go-cmp` | `v0.7.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/google/uuid` | `v1.6.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | `v2.26.3` | simple-cluster-operator, sshkey-operator |
| `github.com/munnerz/goautoneg` | `v0.0.0-20191010083416-a7dc8b61c822` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/pmezard/go-difflib` | `v1.0.0` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/pmezard/go-difflib` | `v1.0.1-0.20181226105442-5d4384ee4fb2` | bind-watcher-operator |
| `github.com/spf13/pflag` | `v1.0.6` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/crypto` | `v0.56.0` | simple-cluster-operator, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/exp` | `v0.0.0-20240719175910-8a7402abbf56` | simple-cluster-operator, sshkey-operator |
| `golang.org/x/net` | `v0.57.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/oauth2` | `v0.36.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/sync` | `v0.22.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/sys` | `v0.47.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/term` | `v0.45.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/text` | `v0.41.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/time` | `v0.11.0` | bind-watcher-operator |
| `golang.org/x/time` | `v0.9.0` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `golang.org/x/tools` | `v0.48.0` | simple-cluster-operator, sshkey-operator |
| `google.golang.org/protobuf` | `v1.36.11` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `gopkg.in/evanphx/json-patch.v4` | `v4.12.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `gopkg.in/inf.v0` | `v0.9.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `lit-html` | `2.8.0` | ui |

### Apache-2.0 AND BSD-3-Clause (13)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/google/cel-go` | `v0.26.0` | simple-cluster-operator, sshkey-operator |
| `github.com/google/pprof` | `v0.0.0-20241029153458-d1b30febd7db` | simple-cluster-operator, sshkey-operator |
| `github.com/prometheus/client_golang` | `v1.22.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.opentelemetry.io/otel` | `v1.43.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/metric` | `v1.43.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/sdk` | `v1.43.0` | simple-cluster-operator, sshkey-operator |
| `go.opentelemetry.io/otel/trace` | `v1.43.0` | bind-watcher-operator, simple-cluster-operator, sshkey-operator |
| `k8s.io/apimachinery` | `v0.34.0` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/apimachinery` | `v0.34.1` | bind-watcher-operator |
| `k8s.io/client-go` | `v0.34.0` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `k8s.io/client-go` | `v0.34.1` | bind-watcher-operator |
| `k8s.io/utils` | `v0.0.0-20250604170112-4c0f3b243397` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/json` | `v0.0.0-20241014173422-cfa47c3a1cc8` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### Apache-2.0 AND MIT (5)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/pelletier/go-toml` | `v1.9.5` | simple-cluster-operator-kind |
| `go.yaml.in/yaml/v2` | `v2.4.2` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `go.yaml.in/yaml/v3` | `v3.0.4` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `gopkg.in/yaml.v3` | `v3.0.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/kind` | `v0.30.0` | simple-cluster-operator-kind |

### Apache-2.0 AND BSD-3-Clause AND MIT (2)

| Dependency | Version | Used by |
|---|---|---|
| `k8s.io/kube-openapi` | `v0.0.0-20250710124328-f3f2b991d03b` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `sigs.k8s.io/yaml` | `v1.6.0` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### ISC (2)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/davecgh/go-spew` | `v1.1.1` | simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |
| `github.com/davecgh/go-spew` | `v1.1.2-0.20180830191138-d8f796af33cc` | bind-watcher-operator |

### BSD-2-Clause (1)

| Dependency | Version | Used by |
|---|---|---|
| `github.com/pkg/errors` | `v0.9.1` | bind-watcher-operator, simple-cluster-operator, simple-cluster-operator-kind, simple-cluster-operator-openstack, simple-cluster-operator-ubuntu, sshkey-operator, ubuntu-machines |

### Unlicense OR Apache-2.0 (1)

| Dependency | Version | Used by |
|---|---|---|
| `@zxing/text-encoding` | `0.9.0` | ui |
