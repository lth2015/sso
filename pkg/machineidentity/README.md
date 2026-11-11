# Online machine identity verifier

`machineidentity.New` creates an immutable, reusable verifier for a trusted SSO HTTPS origin. `Verify(ctx, expectedTenantID, expectedApplicationID, rawBearer)` performs a fresh online primary-bound identity check and returns metadata only. Errors never include the Bearer or upstream body.

See the [complete integration guide](../../docs/external-machine-integration.md), [Go resource example](../../examples/machine-resource/main.go), and [machine contract](../../api/machine.openapi.yaml). Business authorization remains the caller's responsibility.
