# samples/openstack

Standalone smoke test for `simple-cluster-operator-openstack` running
against **CloudFerro WAW4-1**. Creates one Ubuntu 24.04 VM with a
floating IP, plus its SSH keypair Secret.

## Requirements:
* Secret with credentials to OpenStack

## Flow
SimpleCluster --> Openstack-1 SCPC --> OpenstackVM (with Ubuntu) --> UbuntuCluster synced to SimpleCluster

## Simple Cluster Provider Configs:
* openstack-sample TODO

