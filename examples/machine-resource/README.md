# Independent machine resource example

Run from the module root with the trusted authority origin and expected tenant/application IDs described in the [integration guide](../../docs/external-machine-integration.md). The example owns only `127.0.0.1:8092` and exposes `GET /whoami`, `/openapi.yaml` and `/openapi.json`.

The Go verifier performs an online authority check for every request. User cookies and tenant hints provide no authority. The reference endpoint returns identity metadata; apply your own business ACL before granting actual business actions.

The [single-use local client](../machine-client.cjs) reads private AK/SK JSON from stdin and prints only identity metadata. The [reference service OpenAPI](../../api/machine-resource-example.openapi.yaml) is separate from platform management.
