<p align="center">
  <img src="docs/banner.svg" alt="Azure Emulator" width="720">
</p>

# Azure Emulator

A local Microsoft Azure emulator written in Go. The goal is to expose REST
APIs compatible with Azure's management and data-plane endpoints (Storage,
Key Vault, Compute, and others), persist everything in a single embedded
file (BoltDB), and ship with a lightweight web console for inspecting
resources — no Docker, no real Azure subscription, nothing external
required.

Same idea as [gcp-emulator](../gcp-emulator), aimed at Azure instead of
GCP: a portable binary that runs the same way on Windows, Linux, or macOS,
against which you can point the real `az` CLI, the Azure Terraform
provider (`azurerm`), and the official Azure SDKs by overriding their
endpoints to `localhost`.

## Current status

This repo is freshly initialized — no service emulation yet. See
[ROADMAP.md](ROADMAP.md) for the planned phases.

Planned scope (subject to change as work progresses):

- **Storage**: Blob containers/blobs, Queue storage, Table storage.
- **Key Vault**: secrets, keys, certificates.
- **Compute**: virtual machines, virtual networks, disks, images.
- **Resource Manager**: resource groups, subscriptions (fake), ARM-style
  long-running operations.
- **Service Bus**: queues and topics/subscriptions (basic send/receive).
- **Cosmos DB**: SQL API, basic container/document CRUD.
- Web console for browsing emulated resources.

## Project structure

```
cmd/azure-emulator/   entry point, wires up the HTTP server
internal/storage/     embedded persistence (BoltDB) — not yet implemented
internal/queue/       Service Bus-style queue emulation — not yet implemented
internal/server/      router, middlewares, JSON/error helpers — not yet implemented
docs/                  banner and other documentation assets
```

## Requirements

- Go 1.22+
- Azure CLI / Terraform (optional, to exercise real commands against the
  emulator once it has endpoints implemented)

> Note: this repo does not bundle the Go toolchain. If you don't have it
> installed, get it from https://go.dev/dl/ (or `winget install GoLang.Go`
> on Windows, `brew install go` on macOS, `apt install golang-go` on
> Linux).

## Running

```bash
cd azure-emulator
go run ./cmd/azure-emulator
```

By default it listens on `:10000`. This is currently a stub — it starts
and logs that no services are wired up yet.

## License

MIT — see [LICENSE](LICENSE).
