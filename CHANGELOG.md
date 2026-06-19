# Changelog

## [0.16.5](https://github.com/apeirora/showroom-simple-cluster/compare/v0.16.4...v0.16.5) (2026-06-19)


### Bug Fixes

* retry partial k3s install (probe service active, not binary) ([e5b1fa7](https://github.com/apeirora/showroom-simple-cluster/commit/e5b1fa7330391ed58eddea091211e0f78ce370d0))

## [0.16.4](https://github.com/apeirora/showroom-simple-cluster/compare/v0.16.3...v0.16.4) (2026-06-19)


### Bug Fixes

* trim newline on sshkey publicKey in openstack scpc ([9592ca4](https://github.com/apeirora/showroom-simple-cluster/commit/9592ca40f0ffcea438589becab08b7c83b53da35))

## [0.16.3](https://github.com/apeirora/showroom-simple-cluster/compare/v0.16.2...v0.16.3) (2026-06-19)


### Bug Fixes

* arsys user/password ([2c079f5](https://github.com/apeirora/showroom-simple-cluster/commit/2c079f58b107a5a988ab63d2baf3694f56b44b73))
* details UI fix ([9c93475](https://github.com/apeirora/showroom-simple-cluster/commit/9c9347531f566d3befcd878b577d01a5404a89d6))
* rbac fix ([42ec3c5](https://github.com/apeirora/showroom-simple-cluster/commit/42ec3c522709772e3e6f24c1b835a3570a016f0e))
* sap reduce shoot name length ([3739e78](https://github.com/apeirora/showroom-simple-cluster/commit/3739e7846408b591945a38e18e9c26f8d74e036f))
* simple-cluster-operator memory increased ([f14bb3d](https://github.com/apeirora/showroom-simple-cluster/commit/f14bb3da4c58c82c24f420b7dfcc4c1ea176827a))

## [0.16.2](https://github.com/apeirora/showroom-simple-cluster/compare/v0.16.1...v0.16.2) (2026-06-18)


### Bug Fixes

* dati reduce shoot name length ([54cce36](https://github.com/apeirora/showroom-simple-cluster/commit/54cce36b803fa2023425a2c74610eb2b1dd66183))
* require control plane create fields and move above workers ([36bee01](https://github.com/apeirora/showroom-simple-cluster/commit/36bee0141fe7bf12dd8b21752c79b7872cf687f0))
* ui nodes list ([de5ebb5](https://github.com/apeirora/showroom-simple-cluster/commit/de5ebb572258ead738937796eecce0608961ab9c))

## [0.16.1](https://github.com/apeirora/showroom-simple-cluster/compare/v0.16.0...v0.16.1) (2026-06-18)


### Bug Fixes

* build ([b2a4a23](https://github.com/apeirora/showroom-simple-cluster/commit/b2a4a23560f9b343b095261e4097e8baf58e5965))

## [0.16.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.15.2...v0.16.0) (2026-06-18)


### Features

* expose cluster nodes and IPs in SimpleCluster status and portal ([28e2683](https://github.com/apeirora/showroom-simple-cluster/commit/28e2683edbf9c500925225c96809848f91795613))
* implement sshkey-operator ([66eed21](https://github.com/apeirora/showroom-simple-cluster/commit/66eed21e888c92f12088db7ec8d33ff1de506415))
* use SSHKey resource for VM SSH keys via sshKeyRef ([c8e7b9a](https://github.com/apeirora/showroom-simple-cluster/commit/c8e7b9aa880ea1d242f9de61bd2840faff06b15e))


### Bug Fixes

* dati configuration ([39ad9b1](https://github.com/apeirora/showroom-simple-cluster/commit/39ad9b1041b31a5b4006dbc3c7318674cd38c945))

## [0.15.2](https://github.com/apeirora/showroom-simple-cluster/compare/v0.15.1...v0.15.2) (2026-06-14)


### Bug Fixes

* arsys firewall ([6e3d270](https://github.com/apeirora/showroom-simple-cluster/commit/6e3d2703d86f8a0723ad4cf1527e9bba0cfb425d))

## [0.15.1](https://github.com/apeirora/showroom-simple-cluster/compare/v0.15.0...v0.15.1) (2026-06-14)


### Bug Fixes

* force set node name ([6068e30](https://github.com/apeirora/showroom-simple-cluster/commit/6068e30ba493cf65ba7611cff10b79ed4b99d21e))

## [0.15.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.14.1...v0.15.0) (2026-06-14)


### Features

* keep arsys password secret in dedicated namespace ([2ed0369](https://github.com/apeirora/showroom-simple-cluster/commit/2ed0369bae340f4da0bb42ef73c970cd9c6dd604))
* ubuntuCluster user/password support ([4831793](https://github.com/apeirora/showroom-simple-cluster/commit/4831793b9661462893f3bd372f32e2495de2f85e))

## [0.14.1](https://github.com/apeirora/showroom-simple-cluster/compare/v0.14.0...v0.14.1) (2026-06-13)


### Bug Fixes

* Chart version ([f64bb56](https://github.com/apeirora/showroom-simple-cluster/commit/f64bb56155449134ef18e5ee373f6ad02c2107a8))
* public key ([6f4f78d](https://github.com/apeirora/showroom-simple-cluster/commit/6f4f78d93ba4ce110e94b0e4bb63c5e9a364ae92))

## [0.14.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.13.2...v0.14.0) (2026-06-13)


### Features

* inject SSH public key from Secret into Arsys server at order time ([ab9cc89](https://github.com/apeirora/showroom-simple-cluster/commit/ab9cc89a4d6340c34d80f27fb3c98654913d8d58))


### Bug Fixes

* add leases and events RBAC to arsys-ubuntu-machine-operator ([15d4b07](https://github.com/apeirora/showroom-simple-cluster/commit/15d4b078a2bcc1bf623eb872ee9ec662c7b58ee9))
* add leases RBAC to ubuntu-machines-operator ([4f3fb99](https://github.com/apeirora/showroom-simple-cluster/commit/4f3fb9963e7fce0ea1497c8821f083f57d51ccd7))
* cloudferro - workers public IP ([f317562](https://github.com/apeirora/showroom-simple-cluster/commit/f3175626647378e71b8552a7b5d2ec927780034e))
* machine name CRD ([a3b3653](https://github.com/apeirora/showroom-simple-cluster/commit/a3b3653d17204bcb52b7d24f291378cb0393cb08))
* MACHINE printcolumn shows status.serverName ([f2be041](https://github.com/apeirora/showroom-simple-cluster/commit/f2be0413fb0292556f55f60b99cf5944782d20d6))
* remove AUM finalizer when Arsys server is already REMOVING ([9b9959c](https://github.com/apeirora/showroom-simple-cluster/commit/9b9959c6d4b5e9dba4be47ea0f182a43e84eff27))
* skip externalAddress for vmworker when floatingIP is false ([17cfab6](https://github.com/apeirora/showroom-simple-cluster/commit/17cfab6daf139323f886e54d149cfa57f3b9fa77))
* UI all fields required ([debf106](https://github.com/apeirora/showroom-simple-cluster/commit/debf1060239d40acde216aab9c1a6169ff8d1fea))

## [0.13.2](https://github.com/apeirora/showroom-simple-cluster/compare/v0.13.1...v0.13.2) (2026-06-13)


### Bug Fixes

* make sure that charts version X always deploy docker images in version X. ([22bccc0](https://github.com/apeirora/showroom-simple-cluster/commit/22bccc013f3864d644dd88f0ce150c5b976a92cc))

## [0.13.1](https://github.com/apeirora/showroom-simple-cluster/compare/v0.13.0...v0.13.1) (2026-06-13)


### Bug Fixes

* name too long ([adc5b16](https://github.com/apeirora/showroom-simple-cluster/commit/adc5b16bc155ea78bfa31e5513bae9f60ffe5808))

## [0.13.0](https://github.com/apeirora/showroom-simple-cluster/compare/v0.12.0...v0.13.0) (2026-06-13)


### Features

* arsys server name with prefix ([a63a4f9](https://github.com/apeirora/showroom-simple-cluster/commit/a63a4f9f2bed7f842b7aa587925469dea6d29459))
* External IP for arsys workers ([94a026c](https://github.com/apeirora/showroom-simple-cluster/commit/94a026c78e407a745fb3e070033a318530d034c1))

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
