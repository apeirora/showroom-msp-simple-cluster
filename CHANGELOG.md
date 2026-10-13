# Changelog

## [0.39.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.38.3...v0.39.0) (2026-10-13)


### Features

* add optional OpenBao KMS v2 encryption for k3s clusters ([#197](https://github.com/apeirora/showroom-msp-simple-cluster/issues/197)) ([89d1830](https://github.com/apeirora/showroom-msp-simple-cluster/commit/89d1830cb1983b5aba4592f5472200db1293a592))


### Bug Fixes

* **deps:** update Go to 1.27.2 and x/net to v0.60.0 ([#201](https://github.com/apeirora/showroom-msp-simple-cluster/issues/201)) ([3f81b64](https://github.com/apeirora/showroom-msp-simple-cluster/commit/3f81b641ab50c9b066e0e6e022e2fa7d447c4429))
* mount whole KCP credential Secrets for certificate rotation ([#200](https://github.com/apeirora/showroom-msp-simple-cluster/issues/200)) ([e72c6f6](https://github.com/apeirora/showroom-msp-simple-cluster/commit/e72c6f66f132200ff8fb8929347fd6263acb52ef))

## [0.38.3](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.38.2...v0.38.3) (2026-09-24)


### Bug Fixes

* bump builder image to go 1.27.1 ([#192](https://github.com/apeirora/showroom-msp-simple-cluster/issues/192)) ([4433d43](https://github.com/apeirora/showroom-msp-simple-cluster/commit/4433d438cc519ffae2a790457027594f576afa15))
* remove anonymous bind RBAC from pm-integration chart ([#195](https://github.com/apeirora/showroom-msp-simple-cluster/issues/195)) ([6e44366](https://github.com/apeirora/showroom-msp-simple-cluster/commit/6e44366cd1434e44172965f9d12b49816e3d5133))

## [0.38.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.38.1...v0.38.2) (2026-09-23)


### Bug Fixes

* dependencies ([#188](https://github.com/apeirora/showroom-msp-simple-cluster/issues/188)) ([65bf6d5](https://github.com/apeirora/showroom-msp-simple-cluster/commit/65bf6d5bd9b70a7ec7c7d7d7a4f2af40e71d38a5))

## [0.38.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.38.0...v0.38.1) (2026-09-09)


### Bug Fixes

* **ci:** hand back image SBOM files with busybox, not the syft image ([#185](https://github.com/apeirora/showroom-msp-simple-cluster/issues/185)) ([0439a5b](https://github.com/apeirora/showroom-msp-simple-cluster/commit/0439a5be5743c7250eb8943cc7e1928225022885))

## [0.38.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.37.1...v0.38.0) (2026-09-09)


### Features

* **ci:** build and test every module and add the compliance gates ([3b3fe1f](https://github.com/apeirora/showroom-msp-simple-cluster/commit/3b3fe1f763545226cdb64d5e2d0e94425e7dbafb))


### Bug Fixes

* **ci:** grant contents write to the called publish workflow ([69322b9](https://github.com/apeirora/showroom-msp-simple-cluster/commit/69322b9310a56f076cbb942d9ed7487587754c25))
* **deps:** bump golang.org/x/crypto to v0.56.0 ([4f296c6](https://github.com/apeirora/showroom-msp-simple-cluster/commit/4f296c67239c0fbac0bd48a18d275637b5ff21a5))
* **deps:** update dependencies and pin the Go toolchain ([cec2b36](https://github.com/apeirora/showroom-msp-simple-cluster/commit/cec2b36036c97c4e1dade73020eff966d72c8d69))
* **release:** drop the sudo assumption in the image SBOM step ([637b727](https://github.com/apeirora/showroom-msp-simple-cluster/commit/637b727b37f8cb41007fddf2e091923c1994ed51))
* **release:** make the published artefacts consistent ([10362fe](https://github.com/apeirora/showroom-msp-simple-cluster/commit/10362fe4eb94202ebd644da13818a78880b2c557))
* resolve the UI namespace from Luigi path params and stop the list spinner when it is absent ([#184](https://github.com/apeirora/showroom-msp-simple-cluster/issues/184)) ([74a1cf0](https://github.com/apeirora/showroom-msp-simple-cluster/commit/74a1cf02028ac161d39c1d81223f1c9c9e458161))
* **sbom:** restore the missing Go dependency licences ([#180](https://github.com/apeirora/showroom-msp-simple-cluster/issues/180)) ([0196b98](https://github.com/apeirora/showroom-msp-simple-cluster/commit/0196b98167f8bc559646dfa64dbcae3aa9779d2e))

## [0.37.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.37.0...v0.37.1) (2026-08-31)


### Bug Fixes

* **ord:** serialize empty dependencies as arrays ([#170](https://github.com/apeirora/showroom-msp-simple-cluster/issues/170)) ([1fb8191](https://github.com/apeirora/showroom-msp-simple-cluster/commit/1fb819117912a5a4718354ae6de149e124c9c5fd))

## [0.37.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.36.0...v0.37.0) (2026-08-31)


### Features

* expose Simple Cluster dependency metadata through ORD ([#164](https://github.com/apeirora/showroom-msp-simple-cluster/issues/164)) ([00655b7](https://github.com/apeirora/showroom-msp-simple-cluster/commit/00655b7f86961536eeaac8ace0a20e98a27897b9))

## [0.36.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.35.0...v0.36.0) (2026-08-31)


### Features

* move the child delete guard from MSP to KCP ([#167](https://github.com/apeirora/showroom-msp-simple-cluster/issues/167)) ([22a81a2](https://github.com/apeirora/showroom-msp-simple-cluster/commit/22a81a272e33beefa0925dcb3fc40201eea94ec2))

## [0.35.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.34.0...v0.35.0) (2026-08-31)


### Features

* Issue 344 ([#165](https://github.com/apeirora/showroom-msp-simple-cluster/issues/165)) ([6c812dd](https://github.com/apeirora/showroom-msp-simple-cluster/commit/6c812dd28dedcc0ec0b4bc8d73336bae15dbbdba))

## [0.34.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.33.2...v0.34.0) (2026-08-21)


### Features

* offer installable extensions when creating a cluster ([#160](https://github.com/apeirora/showroom-msp-simple-cluster/issues/160)) ([7ed0dc6](https://github.com/apeirora/showroom-msp-simple-cluster/commit/7ed0dc627b08afe540299e18581e7dc6f3ba9b53))

## [0.33.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.33.1...v0.33.2) (2026-08-06)


### Bug Fixes

* align ironcore sample SCPC with working shoots ([#158](https://github.com/apeirora/showroom-msp-simple-cluster/issues/158)) ([5c6b073](https://github.com/apeirora/showroom-msp-simple-cluster/commit/5c6b073f616bc78f0a5e6ca2127f45f03db6c1c6))

## [0.33.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.33.0...v0.33.1) (2026-08-04)


### Bug Fixes

* never create target resources while SimpleCluster is being deleted ([#156](https://github.com/apeirora/showroom-msp-simple-cluster/issues/156)) ([1f32573](https://github.com/apeirora/showroom-msp-simple-cluster/commit/1f325733dbc8affd8597d8d4a33889f6b0e90827))

## [0.33.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.32.0...v0.33.0) (2026-08-04)


### Features

* label SimpleCluster children with owning cluster name ([#155](https://github.com/apeirora/showroom-msp-simple-cluster/issues/155)) ([1f63697](https://github.com/apeirora/showroom-msp-simple-cluster/commit/1f6369751cbeca2d29a02470c0e673728403dbb2))
* UI show Deleting phase from metadata.deletionTimestamp ([#153](https://github.com/apeirora/showroom-msp-simple-cluster/issues/153)) ([88acc52](https://github.com/apeirora/showroom-msp-simple-cluster/commit/88acc521f874e6533de73048aa8283ea18d8e547))


### Bug Fixes

* default worker zone wdf4b — wdf4a is decommissioned ([#152](https://github.com/apeirora/showroom-msp-simple-cluster/issues/152)) ([9b7a936](https://github.com/apeirora/showroom-msp-simple-cluster/commit/9b7a936316414aa9d8bedb5e92818aab6df91ed9))

## [0.32.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.31.0...v0.32.0) (2026-08-03)


### Features

* downstream rbac sap dati ([#150](https://github.com/apeirora/showroom-msp-simple-cluster/issues/150)) ([dad2196](https://github.com/apeirora/showroom-msp-simple-cluster/commit/dad2196000e89e2f4d671b2f04d8ee58855c0ddf))

## [0.31.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.30.2...v0.31.0) (2026-08-03)


### Features

* downstream rbac sap dati ([#148](https://github.com/apeirora/showroom-msp-simple-cluster/issues/148)) ([84c0bdd](https://github.com/apeirora/showroom-msp-simple-cluster/commit/84c0bdda69b0a10e05fc8803d1a298f4d844674b))

## [0.30.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.30.1...v0.30.2) (2026-07-29)


### Bug Fixes

* **ui:** simplify sidebar resource labels ([#146](https://github.com/apeirora/showroom-msp-simple-cluster/issues/146)) ([d80cd1c](https://github.com/apeirora/showroom-msp-simple-cluster/commit/d80cd1c8961c1001b0acc0562db97c932dbfd4d0))

## [0.30.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.30.0...v0.30.1) (2026-07-28)


### Bug Fixes

* align provider API groups to *.eu and add top-level README ([#143](https://github.com/apeirora/showroom-msp-simple-cluster/issues/143)) ([42e2bdf](https://github.com/apeirora/showroom-msp-simple-cluster/commit/42e2bdf172731a798cf05aaa3cd4c90aa44f2c4f))

## [0.30.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.29.0...v0.30.0) (2026-07-27)


### Features

* document that ubuntu-machines is currently unwired ([#141](https://github.com/apeirora/showroom-msp-simple-cluster/issues/141)) ([85635bd](https://github.com/apeirora/showroom-msp-simple-cluster/commit/85635bd76488a249a92f298025adb594b3cfc0f2))

## [0.29.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.28.0...v0.29.0) (2026-07-26)


### Features

* ironcore vm resource for sap integration ([#138](https://github.com/apeirora/showroom-msp-simple-cluster/issues/138)) ([6cd6b09](https://github.com/apeirora/showroom-msp-simple-cluster/commit/6cd6b092f69e9a90b820b3ccfd2e7577146fadcb))

## [0.28.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.27.3...v0.28.0) (2026-07-26)


### Features

* plain gardener shoot spec for provider shoots ([#136](https://github.com/apeirora/showroom-msp-simple-cluster/issues/136)) ([f8fe4dc](https://github.com/apeirora/showroom-msp-simple-cluster/commit/f8fe4dc52bbf5a9d7a2fcaa1fc0a338289647fd7))

## [0.27.3](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.27.2...v0.27.3) (2026-07-26)


### Bug Fixes

* UI drop provider configs column, drop ssh key hint ([#134](https://github.com/apeirora/showroom-msp-simple-cluster/issues/134)) ([53e3eb9](https://github.com/apeirora/showroom-msp-simple-cluster/commit/53e3eb9578a309bd78ed71bcd429fd1ebe7a9d3b))

## [0.27.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.27.1...v0.27.2) (2026-07-26)


### Bug Fixes

* sap scpc shoot names exceed gardener length limit ([#130](https://github.com/apeirora/showroom-msp-simple-cluster/issues/130)) ([867b1d8](https://github.com/apeirora/showroom-msp-simple-cluster/commit/867b1d83d2f7e169eaeeea21d5cef68ca67bd58c))

## [0.27.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.27.0...v0.27.1) (2026-07-26)


### Bug Fixes

* provider drop-down sources for all installed providers ([#128](https://github.com/apeirora/showroom-msp-simple-cluster/issues/128)) ([5ebb609](https://github.com/apeirora/showroom-msp-simple-cluster/commit/5ebb609e60882c339218b50b9d85be5cf1a17c36))

## [0.27.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.26.0...v0.27.0) (2026-07-26)


### Features

* surface vm ssh key and password to workspace users ([#126](https://github.com/apeirora/showroom-msp-simple-cluster/issues/126)) ([f364408](https://github.com/apeirora/showroom-msp-simple-cluster/commit/f364408f1daac56ff9c17d8ab49274c9dcf32e9b))

## [0.26.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.25.0...v0.26.0) (2026-07-26)


### Features

* platform mesh ui for cloudferro and arsys providers ([#124](https://github.com/apeirora/showroom-msp-simple-cluster/issues/124)) ([1d9dab9](https://github.com/apeirora/showroom-msp-simple-cluster/commit/1d9dab9021121879250ba4b2318e4341e95a8bec))

## [0.25.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.24.2...v0.25.0) (2026-07-26)


### Features

* expose OpenStackVM and ArsysUbuntuMachine via provider exports ([#122](https://github.com/apeirora/showroom-msp-simple-cluster/issues/122)) ([d648928](https://github.com/apeirora/showroom-msp-simple-cluster/commit/d648928bc362836656e9033337611810ebe3cc28))

## [0.24.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.24.1...v0.24.2) (2026-07-26)


### Bug Fixes

* shorten generated shoot names to fit Gardener limit ([#120](https://github.com/apeirora/showroom-msp-simple-cluster/issues/120)) ([97b851a](https://github.com/apeirora/showroom-msp-simple-cluster/commit/97b851a12f376ee7e4325e0734356c4ed46002af))

## [0.24.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.24.0...v0.24.1) (2026-07-26)


### Bug Fixes

* rotate immutable APIResourceSchemas via schemaRevision ([#117](https://github.com/apeirora/showroom-msp-simple-cluster/issues/117)) ([0b80ccb](https://github.com/apeirora/showroom-msp-simple-cluster/commit/0b80ccbbe142857f1227c09d502493f02965b997))
* rotate immutable APIResourceSchemas via schemaRevision ([#118](https://github.com/apeirora/showroom-msp-simple-cluster/issues/118)) ([0549949](https://github.com/apeirora/showroom-msp-simple-cluster/commit/0549949720e7747bc91df6094c9f417e31915427))

## [0.24.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.23.3...v0.24.0) (2026-07-25)


### Features

* providers: arsys & cloudferro ([#112](https://github.com/apeirora/showroom-msp-simple-cluster/issues/112)) ([780a938](https://github.com/apeirora/showroom-msp-simple-cluster/commit/780a938e64c7f1589cba62a7bcfaae9e66a79d7d))
* sap/dati provider: rebrand labels + per-provider icons ([#110](https://github.com/apeirora/showroom-msp-simple-cluster/issues/110)) ([2200caa](https://github.com/apeirora/showroom-msp-simple-cluster/commit/2200caa369202473840261941179b42cb88f734d))

## [0.23.3](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.23.2...v0.23.3) (2026-07-24)


### Bug Fixes

* sap provider - go version 1.24 -&gt; 1.25 ([#108](https://github.com/apeirora/showroom-msp-simple-cluster/issues/108)) ([1071ae7](https://github.com/apeirora/showroom-msp-simple-cluster/commit/1071ae7e2a701c587c6b514c79d5997d70af12fe))

## [0.23.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.23.1...v0.23.2) (2026-07-24)


### Bug Fixes

* sapprovider CRD metadata.name must be plural.group ([#106](https://github.com/apeirora/showroom-msp-simple-cluster/issues/106)) ([368c65a](https://github.com/apeirora/showroom-msp-simple-cluster/commit/368c65ad6fdbc7b37484bf038ec383972b75f66e))

## [0.23.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.23.0...v0.23.1) (2026-07-24)


### Bug Fixes

* Sap provider ci fix ([#104](https://github.com/apeirora/showroom-msp-simple-cluster/issues/104)) ([4c998d5](https://github.com/apeirora/showroom-msp-simple-cluster/commit/4c998d5b8c750dae6f7924c12e00b57f42c6a8aa))

## [0.23.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.22.3...v0.23.0) (2026-07-24)


### Features

* sap provider ([#99](https://github.com/apeirora/showroom-msp-simple-cluster/issues/99)) ([e6a896b](https://github.com/apeirora/showroom-msp-simple-cluster/commit/e6a896bba30c9704fabf73dd4dfe1a96e8e004b6))

## [0.22.3](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.22.2...v0.22.3) (2026-07-23)


### Bug Fixes

* UI fix ([#95](https://github.com/apeirora/showroom-msp-simple-cluster/issues/95)) ([dfe2310](https://github.com/apeirora/showroom-msp-simple-cluster/commit/dfe2310bd6b7fd789bf43dd8ebb47ce7d73f4f41))

## [0.22.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.22.1...v0.22.2) (2026-07-23)


### Bug Fixes

* scpc template ([#93](https://github.com/apeirora/showroom-msp-simple-cluster/issues/93)) ([51064d1](https://github.com/apeirora/showroom-msp-simple-cluster/commit/51064d1c02b279533b1e2787ee3ac20b088e4224))

## [0.22.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.22.0...v0.22.1) (2026-07-23)


### Bug Fixes

* bump operator Docker base images to golang 1.25 ([#91](https://github.com/apeirora/showroom-msp-simple-cluster/issues/91)) ([b441343](https://github.com/apeirora/showroom-msp-simple-cluster/commit/b4413436e4f38f74e2af329e43125044c063c5f5))

## [0.22.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.21.2...v0.22.0) (2026-07-23)


### Features

* Bind detect operator ([#89](https://github.com/apeirora/showroom-msp-simple-cluster/issues/89)) ([46956fb](https://github.com/apeirora/showroom-msp-simple-cluster/commit/46956fbc9956f69b9ff0b57149994bd9c416610a))

## [0.21.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.21.1...v0.21.2) (2026-07-21)


### Bug Fixes

* drop unsupported permissionClaims.all from dati APIExport ([#82](https://github.com/apeirora/showroom-msp-simple-cluster/issues/82)) ([ee864b9](https://github.com/apeirora/showroom-msp-simple-cluster/commit/ee864b9fae16b5193b154f8868174f7986f69255))

## [0.21.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.21.0...v0.21.1) (2026-07-21)


### Bug Fixes

* dati marketplace ([#80](https://github.com/apeirora/showroom-msp-simple-cluster/issues/80)) ([4dcff61](https://github.com/apeirora/showroom-msp-simple-cluster/commit/4dcff6192bdac7417c5135c2156c56e384c41c53))

## [0.21.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.20.2...v0.21.0) (2026-07-21)


### Features

* rename marketplace integration to 'Dati integration' ([#78](https://github.com/apeirora/showroom-msp-simple-cluster/issues/78)) ([491f963](https://github.com/apeirora/showroom-msp-simple-cluster/commit/491f963dca48e0570139ab5e2b78e4205010e205))

## [0.20.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.20.1...v0.20.2) (2026-07-06)


### Bug Fixes

* UI table fix ([6d6b705](https://github.com/apeirora/showroom-msp-simple-cluster/commit/6d6b7053f711893498b256d29ba74a8980cc5d71))

## [0.20.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.20.0...v0.20.1) (2026-07-06)


### Bug Fixes

* JSON-encode quantity values in create mutation ([b599485](https://github.com/apeirora/showroom-msp-simple-cluster/commit/b5994852fd57bac3912143f3ef2c51c510591600))

## [0.20.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.19.2...v0.20.0) (2026-07-06)


### Features

* worker pool autoscaling with min/max range ([c025272](https://github.com/apeirora/showroom-msp-simple-cluster/commit/c0252725f17991b509a2feb70d5fde33cbc9a57b))

## [0.19.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.19.1...v0.19.2) (2026-07-04)


### Bug Fixes

* 404 graphql ([ef72b5e](https://github.com/apeirora/showroom-msp-simple-cluster/commit/ef72b5e332972af5162e2bfe9b8a87702f5e12fd))

## [0.19.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.19.0...v0.19.1) (2026-07-01)


### Bug Fixes

* **cloudferro:** scope SSH key export secret name per SimpleCluster ([44c7879](https://github.com/apeirora/showroom-msp-simple-cluster/commit/44c787941dde6188b13f002fee7e8e71ad9958ff))

## [0.19.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.18.0...v0.19.0) (2026-06-30)


### Features

* allow public access to nginx ingress LB ([2460300](https://github.com/apeirora/showroom-msp-simple-cluster/commit/2460300d53d8fe874b24a3ed30a25b659343161f))
* arsys dedicated servers disabled ([91f0668](https://github.com/apeirora/showroom-msp-simple-cluster/commit/91f0668e8029b4db21592368dd577b8185004bfb)), closes [#277](https://github.com/apeirora/showroom-msp-simple-cluster/issues/277)
* arsys dedicated servers disabled ([eea6d88](https://github.com/apeirora/showroom-msp-simple-cluster/commit/eea6d8875d66ec618c4f1ad23ecd5b07e4c7c9c2)), closes [#277](https://github.com/apeirora/showroom-msp-simple-cluster/issues/277)
* install ingress by default ([79d0cbe](https://github.com/apeirora/showroom-msp-simple-cluster/commit/79d0cbe17a7892d666c8c0bd730a313b5cbf5b52))
* remove ingress to release port 443 ([96b962e](https://github.com/apeirora/showroom-msp-simple-cluster/commit/96b962e2f9d779faeb42fd2373dead4c81f20097))
* remove ingress to release port 443 ([b2d71ae](https://github.com/apeirora/showroom-msp-simple-cluster/commit/b2d71ae4b517d6084bb2ea736f91fabe35187d02))
* **ubuntu-machines:** per-claim SSH key injection via sshkey-operator ([3829d76](https://github.com/apeirora/showroom-msp-simple-cluster/commit/3829d7626f19dfec80e79425f5f4e0f76f21e8fd))


### Bug Fixes

* **arsys:** rename SSH to AdminSSH in UbuntuMachine spec, tidy go.sum ([f81adc7](https://github.com/apeirora/showroom-msp-simple-cluster/commit/f81adc73b8b2ce402127fc86d5f92a0308be2667))

## [0.18.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.17.0...v0.18.0) (2026-06-22)


### Features

* open pots 80 and 443 ([a06b831](https://github.com/apeirora/showroom-msp-simple-cluster/commit/a06b83113fce839fe2aaa14f77c848b9b6ebc447))


### Bug Fixes

* dati shoot length reduced ([34a91da](https://github.com/apeirora/showroom-msp-simple-cluster/commit/34a91dab4718faaa3c1748633c582c11a65c0b78))
* sap reduce shoot name length ([a269519](https://github.com/apeirora/showroom-msp-simple-cluster/commit/a269519f7d88ee500f1e0d7e5b183720e585161a))

## [0.17.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.16.5...v0.17.0) (2026-06-19)


### Features

* set worker externalAddress for arsys-virtual; bump scpc-config 0.11.2 ([e7e6aab](https://github.com/apeirora/showroom-msp-simple-cluster/commit/e7e6aab5255ab26cff53ed354022fc9cf1e2ac90))


### Bug Fixes

* sap reduce shoot name length ([180c1d7](https://github.com/apeirora/showroom-msp-simple-cluster/commit/180c1d7def3fe29884cf46eb2ce4f4a6479c8ab7))

## [0.16.5](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.16.4...v0.16.5) (2026-06-19)


### Bug Fixes

* retry partial k3s install (probe service active, not binary) ([671e994](https://github.com/apeirora/showroom-msp-simple-cluster/commit/671e994ec6a4073293863b02461552399ae53f61))

## [0.16.4](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.16.3...v0.16.4) (2026-06-19)


### Bug Fixes

* trim newline on sshkey publicKey in openstack scpc ([6370808](https://github.com/apeirora/showroom-msp-simple-cluster/commit/63708082934dc5ba8b1125b55e271a27bf74ca2f))

## [0.16.3](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.16.2...v0.16.3) (2026-06-19)


### Bug Fixes

* arsys user/password ([cfec871](https://github.com/apeirora/showroom-msp-simple-cluster/commit/cfec871547eb2018923d98427af04d0a08846e21))
* details UI fix ([b3e2ea8](https://github.com/apeirora/showroom-msp-simple-cluster/commit/b3e2ea8e8fd160ec0a3794bb1a078b9b142ab2d8))
* rbac fix ([cfbcf07](https://github.com/apeirora/showroom-msp-simple-cluster/commit/cfbcf07c14fc350b8cfa5e1543f51bdd5937085b))
* sap reduce shoot name length ([9ab4cbc](https://github.com/apeirora/showroom-msp-simple-cluster/commit/9ab4cbc8edeecf174cf851a73b631e96fe6ab1a7))
* simple-cluster-operator memory increased ([91ab024](https://github.com/apeirora/showroom-msp-simple-cluster/commit/91ab024f6d49704ffc36c0b30d715513758788ed))

## [0.16.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.16.1...v0.16.2) (2026-06-18)


### Bug Fixes

* dati reduce shoot name length ([068f517](https://github.com/apeirora/showroom-msp-simple-cluster/commit/068f517dd56f4cf9626a5c0b9509979f9126ec7d))
* require control plane create fields and move above workers ([63c464b](https://github.com/apeirora/showroom-msp-simple-cluster/commit/63c464b69305f034af02db079e0e996eb4397dbd))
* ui nodes list ([06e3cf8](https://github.com/apeirora/showroom-msp-simple-cluster/commit/06e3cf81c595db16f6c777049d61c09b6c9e475c))

## [0.16.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.16.0...v0.16.1) (2026-06-18)


### Bug Fixes

* build ([972104a](https://github.com/apeirora/showroom-msp-simple-cluster/commit/972104a5341edac2ef110dc70f6abc387bb7636b))

## [0.16.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.15.2...v0.16.0) (2026-06-18)


### Features

* expose cluster nodes and IPs in SimpleCluster status and portal ([00eff49](https://github.com/apeirora/showroom-msp-simple-cluster/commit/00eff49deeb8b197f7b05e81794b83446c8afe62))
* implement sshkey-operator ([270e2d0](https://github.com/apeirora/showroom-msp-simple-cluster/commit/270e2d0e990a2d8f5f524ac878225645d9731b38))
* use SSHKey resource for VM SSH keys via sshKeyRef ([fb19439](https://github.com/apeirora/showroom-msp-simple-cluster/commit/fb19439dc705086674406671e234b38a1eb54a8d))


### Bug Fixes

* dati configuration ([c98f353](https://github.com/apeirora/showroom-msp-simple-cluster/commit/c98f3536c4909c4d2a58a6d7446282155ff3a498))

## [0.15.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.15.1...v0.15.2) (2026-06-14)


### Bug Fixes

* arsys firewall ([c168469](https://github.com/apeirora/showroom-msp-simple-cluster/commit/c1684695d722210f2a41f91c34191b96db78b01f))

## [0.15.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.15.0...v0.15.1) (2026-06-14)


### Bug Fixes

* force set node name ([781fd29](https://github.com/apeirora/showroom-msp-simple-cluster/commit/781fd290e0a8243319555d67e865f0768bf55be6))

## [0.15.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.14.1...v0.15.0) (2026-06-14)


### Features

* keep arsys password secret in dedicated namespace ([6239001](https://github.com/apeirora/showroom-msp-simple-cluster/commit/6239001948f449e90746edaa7a23f7c16c63c4ca))
* ubuntuCluster user/password support ([80fff5c](https://github.com/apeirora/showroom-msp-simple-cluster/commit/80fff5c5224084aa1c9fda3e493cbdcdd9558867))

## [0.14.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.14.0...v0.14.1) (2026-06-13)


### Bug Fixes

* Chart version ([0c98dc7](https://github.com/apeirora/showroom-msp-simple-cluster/commit/0c98dc7bbfbb890f7d8256896879e84ca19752ae))
* public key ([29400f3](https://github.com/apeirora/showroom-msp-simple-cluster/commit/29400f3bffec2dd7f73cb318ad9bd830cb9e1109))

## [0.14.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.13.2...v0.14.0) (2026-06-13)


### Features

* inject SSH public key from Secret into Arsys server at order time ([e5e053e](https://github.com/apeirora/showroom-msp-simple-cluster/commit/e5e053e8ae6b01ad12416145f34f82c07f55e832))


### Bug Fixes

* add leases and events RBAC to arsys-ubuntu-machine-operator ([cf13a5f](https://github.com/apeirora/showroom-msp-simple-cluster/commit/cf13a5fd038c294dec78c8f76cfb6df22f162aac))
* add leases RBAC to ubuntu-machines-operator ([dcc1eca](https://github.com/apeirora/showroom-msp-simple-cluster/commit/dcc1ecae4ff617a6bca63e6cf54421c42840dcb5))
* cloudferro - workers public IP ([98defae](https://github.com/apeirora/showroom-msp-simple-cluster/commit/98defae78ad3674a4a949d965eee1e207891a26e))
* machine name CRD ([abaccca](https://github.com/apeirora/showroom-msp-simple-cluster/commit/abaccca4c3565aaee8a51e82c5a98d55a37d5403))
* MACHINE printcolumn shows status.serverName ([368a106](https://github.com/apeirora/showroom-msp-simple-cluster/commit/368a1069b3d2b466fbb64b7b5435de25b7ffbe63))
* remove AUM finalizer when Arsys server is already REMOVING ([0ae86fa](https://github.com/apeirora/showroom-msp-simple-cluster/commit/0ae86fa41edc4aa6aca7e8184d4e3b17e743e34e))
* skip externalAddress for vmworker when floatingIP is false ([c452149](https://github.com/apeirora/showroom-msp-simple-cluster/commit/c452149e063021d8a71171e38b4ae899b229bfd7))
* UI all fields required ([7e6597c](https://github.com/apeirora/showroom-msp-simple-cluster/commit/7e6597c2eb14788b02f46b67e87b6bb3f39258da))

## [0.13.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.13.1...v0.13.2) (2026-06-13)


### Bug Fixes

* make sure that charts version X always deploy docker images in version X. ([27f0fa7](https://github.com/apeirora/showroom-msp-simple-cluster/commit/27f0fa71a796e231301376471972de8b1046eab2))

## [0.13.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.13.0...v0.13.1) (2026-06-13)


### Bug Fixes

* name too long ([d6d01ae](https://github.com/apeirora/showroom-msp-simple-cluster/commit/d6d01aefcc0163106131256a16522f2b2f28f023))

## [0.13.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.12.0...v0.13.0) (2026-06-13)


### Features

* arsys server name with prefix ([11db5ff](https://github.com/apeirora/showroom-msp-simple-cluster/commit/11db5ff6a53bc675e8a939feb4345fefc32d66c2))
* External IP for arsys workers ([ba510da](https://github.com/apeirora/showroom-msp-simple-cluster/commit/ba510dafa9b5d080b670d6b2198a7055571f096a))

## [0.12.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.11.0...v0.12.0) (2026-06-12)


### Features

* **arsys:** API events, transient retry counter, deferred failure ([e3f2c30](https://github.com/apeirora/showroom-msp-simple-cluster/commit/e3f2c30e63c6a06895e52fd0efd31ac446035451))
* External IP for cloudferro workers ([#30](https://github.com/apeirora/showroom-msp-simple-cluster/issues/30)) ([bd6cd7f](https://github.com/apeirora/showroom-msp-simple-cluster/commit/bd6cd7fc88b2e1f786b0d1dd6024c859142f60bb))
* **ubuntu:** pass --node-external-ip when UbuntuCluster host has ExternalAddress ([85844c5](https://github.com/apeirora/showroom-msp-simple-cluster/commit/85844c55489807ea885270cdc4363598d8f7c7cc))


### Bug Fixes

* **arsys:** backward-compatible server adoption + scpc address field ([17a5213](https://github.com/apeirora/showroom-msp-simple-cluster/commit/17a52137285abba4c6ca6cce76cc6dad36d310b9))

## [0.11.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.10.5...v0.11.0) (2026-06-12)


### Features

* **controller:** register host external IPs on k3s nodes ([#26](https://github.com/apeirora/showroom-msp-simple-cluster/issues/26)) ([96a0f27](https://github.com/apeirora/showroom-msp-simple-cluster/commit/96a0f27f2ba8a74a783eb262764a4cd7a6bcc818))


### Bug Fixes

* SimpleCluster: arsysubuntumachine with progress ([1bbfb58](https://github.com/apeirora/showroom-msp-simple-cluster/commit/1bbfb588279af14ac456a19c3a8b8f40c9588b03))

## [0.10.5](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.10.4...v0.10.5) (2026-06-12)


### Bug Fixes

* SimpleCluster: config version ([13b5408](https://github.com/apeirora/showroom-msp-simple-cluster/commit/13b5408daba9689603595dd5b92bd6da1d6d94b1))
* SimpleCluster: too many request should not change state to field. ([6860015](https://github.com/apeirora/showroom-msp-simple-cluster/commit/6860015762893e1428326046d95141b491aee52e))

## [0.10.4](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.10.3...v0.10.4) (2026-06-12)


### Bug Fixes

* SimpleCluster: dati config fix ([64c5b9c](https://github.com/apeirora/showroom-msp-simple-cluster/commit/64c5b9cf9998cc2d60194600f2ac14cce60be68b))
* SimpleCluster: update status ([5b2bfd1](https://github.com/apeirora/showroom-msp-simple-cluster/commit/5b2bfd12abed8685b22811b3645365fa5f979cf7))

## [0.10.3](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.10.2...v0.10.3) (2026-06-12)


### Bug Fixes

* SimpleCluster: arsys update status from API to resource ([5a37a3e](https://github.com/apeirora/showroom-msp-simple-cluster/commit/5a37a3e0ed8791d6a426bfe6604bad2e8b266cc0))

## [0.10.2](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.10.1...v0.10.2) (2026-06-12)


### Bug Fixes

* SimpleCluster: arsys ([b47a377](https://github.com/apeirora/showroom-msp-simple-cluster/commit/b47a3772fb56f0dd753ba91750cc38a28135643d))

## [0.10.1](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.10.0...v0.10.1) (2026-06-11)


### Bug Fixes

* SimpleCluster: Machines are not actually deleted when a cluster is deleted — deletion is assumed complete once the request is accepted ([#19](https://github.com/apeirora/showroom-msp-simple-cluster/issues/19)) ([bf59955](https://github.com/apeirora/showroom-msp-simple-cluster/commit/bf599555f8b1a64f57934c827e26f1b2021e7f59))
* SimpleCluster: update config chart ([a9cc2f0](https://github.com/apeirora/showroom-msp-simple-cluster/commit/a9cc2f0eccf186b900d8eb4a25170ee4824f0edd))

## [0.10.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.9.0...v0.10.0) (2026-06-11)


### Features

* **operator:** label kubeconfig Secrets with apeirora.eu/kubeconfig ([#15](https://github.com/apeirora/showroom-msp-simple-cluster/issues/15)) ([9c243b7](https://github.com/apeirora/showroom-msp-simple-cluster/commit/9c243b7397720fd8beff69102a9e10f89c3efce8))
* UbuntuCluster with SSH key from different namespace ([a9ef56d](https://github.com/apeirora/showroom-msp-simple-cluster/commit/a9ef56ddeb9e05019ded1a296893d940e6518308))


### Bug Fixes

* Claudferro config ([9ca22bb](https://github.com/apeirora/showroom-msp-simple-cluster/commit/9ca22bbdc9c0f529c1e266326861c7cc9f31ff8e))
* external secret typo ([c832645](https://github.com/apeirora/showroom-msp-simple-cluster/commit/c8326453d3ea0adfbd49e91a6387c7a2ecf03e46))
* IronCore SAP config ([e2df600](https://github.com/apeirora/showroom-msp-simple-cluster/commit/e2df600a05fe65ce388cc9baaded3905d6f12bbc))
* IronCore SAP config ([43f1249](https://github.com/apeirora/showroom-msp-simple-cluster/commit/43f1249b692757ab4a19ac3c4d3b492df39d37cf))
* rbac ([607ce73](https://github.com/apeirora/showroom-msp-simple-cluster/commit/607ce7312f88a71d50fbbe443b0dfcbe34c65981))

## [0.9.0](https://github.com/apeirora/showroom-msp-simple-cluster/compare/v0.8.4...v0.9.0) (2026-06-11)


### Features

* Arsys autodetect cloud profile ([a1d2650](https://github.com/apeirora/showroom-msp-simple-cluster/commit/a1d26505f95b732fc0296f0bce65a5a44a6246be))
* ironCore ([8059eee](https://github.com/apeirora/showroom-msp-simple-cluster/commit/8059eee53fc3f15624546866880247b006fc952e))
* IronCore ([edfe445](https://github.com/apeirora/showroom-msp-simple-cluster/commit/edfe4458d7bce4a226b9927be9778d7c65583caa))
* IronCore helm chart ([9f59cec](https://github.com/apeirora/showroom-msp-simple-cluster/commit/9f59cec61b1b3370b07d0c3e39a38153298a7939))
* IronCore helm chart ([1661702](https://github.com/apeirora/showroom-msp-simple-cluster/commit/1661702e78e6fc7b3842fb604a310238441d0b6b))


### Bug Fixes

* build arsys image from repo root, multi-arch ([043547c](https://github.com/apeirora/showroom-msp-simple-cluster/commit/043547c1eca42e81cda94b04110da4073b5615d3))
