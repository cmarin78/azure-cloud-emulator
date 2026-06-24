# Security policy

`azure-emulator` is a local development/testing tool that fakes Azure's
ARM control plane and a subset of data planes. It is **not** intended to
be exposed to untrusted networks or used as a production service:

- AAD token issuance (`internal/services/aadtoken`) and the Microsoft
  Graph stub (`internal/services/graph`) are fakes — they accept any
  client id/secret and issue self-signed tokens with no real
  cryptographic guarantees. Do not point real credentials at it beyond
  the fake values the smoke tests use.
- The optional TLS listener (`-tls`) uses a locally generated
  self-signed certificate purely so the real `azurerm` Terraform
  provider's Go TLS stack will talk to it — it provides no real
  transport security guarantee beyond that.
- Persisted state (`.azure-emulator-data/azure-emulator.db`) is an
  unencrypted local BoltDB file. Treat it like any other local dev
  artifact: don't commit it (it's gitignored) and don't assume secrets
  stored through Key Vault/Service Bus/etc. simulated endpoints are
  protected at rest.

## Reporting a vulnerability

If you find a security issue specific to this emulator (e.g., something
that would let it be used to attack a host it's running on, not just
"the fake auth isn't real auth," which is expected), please open a
private report via GitHub's "Report a vulnerability" flow on
[cmarin78/azure-cloud-emulator](https://github.com/cmarin78/azure-cloud-emulator)
rather than a public issue.

There is no bug-bounty program; this is a personal/portfolio project,
not a production service.
