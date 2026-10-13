# Secret reference configuration

The operator checks each Secret reference before it uses the Secret.
An omitted reference namespace means the request namespace.
A reference to another namespace requires an exact operator exception.

Set `secretReferenceAllowlist` in the operator chart values:

```yaml
secretReferenceAllowlist:
  - namespace: credential-system
    name: shared-key
    purpose: use
```

The chart passes the list through `--secret-reference-allowlist` as JSON.
Each entry requires an explicit namespace, name, and purpose.
Wildcards and empty entry namespaces are invalid.

| Purpose | Allowed operation |
| --- | --- |
| `use` | Use the named credential |
| `copy` | Copy data from the named Secret |
| `write` | Create or change the named Secret |

An exception for one purpose does not permit another purpose.
For example, `use` does not permit a copy of the credential.
The default list is empty.
The controller checks still apply if Kubernetes RBAC permits broader access.

UbuntuMachine is cluster scoped.
Its administrator credential has no request namespace default.
Configure an exact `use` entry for every UbuntuMachine administrator Secret.

```yaml
secretReferenceAllowlist:
  - namespace: machine-credentials
    name: machine-admin-ssh
    purpose: use
```

Set the values on the controller chart that needs the exception.
For the system chart, place them under the applicable subchart key.
The SSHKey standalone chart and system subchart use the same list format.
