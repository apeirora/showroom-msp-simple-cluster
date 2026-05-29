# samples/ubuntuMachines

Sample that uses set of Ubuntu machines to build cluster on them.


## Requirements:
* Ubuntu machines with SSH access and sudo.
* Ubuntu machines are defined by creation of `UbuntuMachine`
* Machine must be claimed before it can be used.


## Flow
SimpleCluster --> UbuntuMachines SCPC --> UbuntuMachineClaim(s) --> UbuntuCluster synced to SimpleCluster

## Simple Cluster Provider Configs:
* ubuntu-machines-0
