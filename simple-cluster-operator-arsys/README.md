# simple-cluster-operator-arsys

Operator that orders **dedicated servers from the [Arsys CloudBuilder API](https://api.cloudbuilder.es/documentation/v1/en/api/documentation.html)**
and registers each as a cluster-scoped `UbuntuMachine` it owns. 

See: https://api.cloudbuilder.es/documentation/v1/en/api/documentation.html

## Sizing a dedicated server

For `server.type: dedicated` you either pin a model with `baremetalModelID`, or
leave it empty and give `server.vcore` + `server.ram` (minimums). When the model
is omitted the operator lists `GET /servers/baremetal_models` and orders the
**smallest** `ENABLED` model that meets those minimums and is available in
`server.datacenterID` — fewest cores first, then least RAM. The chosen model and
its real size are written to `status.baremetalModelID` / `status.selectedCores` /
`status.selectedRAMGB`, and the size is mirrored onto the `UbuntuMachine`
capacity. See `samples/arsys-ubuntu-machine/arsysubuntumachine-dedicated-auto.yaml`.