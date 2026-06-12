# Changelog

## [0.12.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.11.0...v0.12.0) (2026-06-12)


### Features

* **arsys:** API events, transient retry counter, deferred failure ([f24537d](https://github.com/apeirora/showroom-simple-cluster/commit/f24537dee8277275911d579e390d90556c6cc48a))
* External IP for cloudferro workers ([#30](https://github.com/apeirora/showroom-simple-cluster/issues/30)) ([f6af261](https://github.com/apeirora/showroom-simple-cluster/commit/f6af26165cb6dd7ec695e51f7afc5fa8e7dc26ed))
* **ubuntu:** pass --node-external-ip when UbuntuCluster host has ExternalAddress ([fe1a68f](https://github.com/apeirora/showroom-simple-cluster/commit/fe1a68ff962a2fe2b15dd7e7b241fcc8a2e1657c))


### Bug Fixes

* **arsys:** backward-compatible server adoption + scpc address field ([577ebbf](https://github.com/apeirora/showroom-simple-cluster/commit/577ebbffd749b22d9dbc1cfe9cafaadb5a017da2))

## [0.11.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.10.5...v0.11.0) (2026-06-12)


### Features

* **controller:** register host external IPs on k3s nodes ([#26](https://github.com/apeirora/showroom-simple-cluster/issues/26)) ([271f5b1](https://github.com/apeirora/showroom-simple-cluster/commit/271f5b155d5222480a7f18a473a913f615a7e4de))


### Bug Fixes

* SimpleCluster: arsysubuntumachine with progress ([f21e001](https://github.com/apeirora/showroom-simple-cluster/commit/f21e001c52e1f21980c77283d1131fdc90d6ea27))

## [0.10.5](https://github.com/apeirora/showroom-simple-cluster/compare/v0.10.4...v0.10.5) (2026-06-12)


### Bug Fixes

* SimpleCluster: config version ([dd985cf](https://github.com/apeirora/showroom-simple-cluster/commit/dd985cff80975c51d84bed0134e1e849d098623f))
* SimpleCluster: too many request should not change state to field. ([9c317e4](https://github.com/apeirora/showroom-simple-cluster/commit/9c317e4985f7bc4848d557268201f04a1979cfbf))

## [0.10.4](https://github.com/apeirora/showroom-simple-cluster/compare/v0.10.3...v0.10.4) (2026-06-12)


### Bug Fixes

* SimpleCluster: dati config fix ([6cc79a6](https://github.com/apeirora/showroom-simple-cluster/commit/6cc79a64b5cbc40c28fc92f6ec31a824dc3d69f8))
* SimpleCluster: update status ([5603178](https://github.com/apeirora/showroom-simple-cluster/commit/56031783fe46aad27b19b9e6dad692176ec89c4c))

## [0.10.3](https://github.com/apeirora/showroom-simple-cluster/compare/v0.10.2...v0.10.3) (2026-06-12)


### Bug Fixes

* SimpleCluster: arsys update status from API to resource ([7bf8a9c](https://github.com/apeirora/showroom-simple-cluster/commit/7bf8a9c8e37a18be911e63fd75c0542c15d3c19b))

## [0.10.2](https://github.com/apeirora/showroom-simple-cluster/compare/v0.10.1...v0.10.2) (2026-06-12)


### Bug Fixes

* SimpleCluster: arsys ([005b309](https://github.com/apeirora/showroom-simple-cluster/commit/005b309407f65d137fb4b9aa48f9ce911df342bc))

## [0.10.1](https://github.com/apeirora/showroom-simple-cluster/compare/v0.10.0...v0.10.1) (2026-06-11)


### Bug Fixes

* SimpleCluster: Machines are not actually deleted when a cluster is deleted — deletion is assumed complete once the request is accepted ([#19](https://github.com/apeirora/showroom-simple-cluster/issues/19)) ([a0a0c31](https://github.com/apeirora/showroom-simple-cluster/commit/a0a0c31346b4be2538345538eb711fdcc98477f0))
* SimpleCluster: update config chart ([f757e12](https://github.com/apeirora/showroom-simple-cluster/commit/f757e1296064073f03f4d9818e1ceee4051dcd53))

## [0.10.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.9.0...v0.10.0) (2026-06-11)


### Features

* **operator:** label kubeconfig Secrets with apeirora.eu/kubeconfig ([#15](https://github.com/apeirora/showroom-simple-cluster/issues/15)) ([d294950](https://github.com/apeirora/showroom-simple-cluster/commit/d29495032921ff98c4ce9901a881437984709ae9))
* UbuntuCluster with SSH key from different namespace ([3625ea0](https://github.com/apeirora/showroom-simple-cluster/commit/3625ea0c2d4a1003a9f18baff59a185835060592))


### Bug Fixes

* Claudferro config ([1177f39](https://github.com/apeirora/showroom-simple-cluster/commit/1177f390b2f60ded399b0bd0a66d671b9fb31e74))
* external secret typo ([26fda43](https://github.com/apeirora/showroom-simple-cluster/commit/26fda43ee4ee582bcf757f980745d58e8c258a08))
* IronCore SAP config ([6d94729](https://github.com/apeirora/showroom-simple-cluster/commit/6d94729e3005de3c540c59ff62e305e46dcf7992))
* IronCore SAP config ([1125907](https://github.com/apeirora/showroom-simple-cluster/commit/1125907d04f907a89b8fe22efc2bc36880688ad6))
* rbac ([e573def](https://github.com/apeirora/showroom-simple-cluster/commit/e573deffda1a5299e1df1e4e23b6100ffbcc5d54))

## [0.9.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.8.4...v0.9.0) (2026-06-11)


### Features

* Arsys autodetect cloud profile ([546b622](https://github.com/apeirora/showroom-simple-cluster/commit/546b6228fe576f11a71c925326b66495f0f9effb))
* ironCore ([66f9bff](https://github.com/apeirora/showroom-simple-cluster/commit/66f9bff3416b3508d47af8390173835c321cf128))
* IronCore ([7ace4aa](https://github.com/apeirora/showroom-simple-cluster/commit/7ace4aa80676fa316705d4f570242fadae6f5d42))
* IronCore helm chart ([5fbdffc](https://github.com/apeirora/showroom-simple-cluster/commit/5fbdffcaa82d2e7299c8d54398e2cc3a7cbcf8d5))
* IronCore helm chart ([d70bf27](https://github.com/apeirora/showroom-simple-cluster/commit/d70bf27a5c0c0c269fb635c48fdbdc0b1dc0d001))


### Bug Fixes

* build arsys image from repo root, multi-arch ([4adfe75](https://github.com/apeirora/showroom-simple-cluster/commit/4adfe757d92885306622cd765a47e4378aea66b9))
