# Roadmap

Status: repo initialized, no service emulation implemented yet.

## Phase 0 — Bootstrap (done)

- Repo structure, go.mod, README, banner, license.

## Phase 1 — Core server

- HTTP router + middleware (`internal/server`).
- Embedded persistence with BoltDB (`internal/storage`).
- Health/version endpoint.

## Phase 2 — Storage

- Blob containers/blobs (CRUD, upload/download).
- Queue storage (CRUD, enqueue/dequeue).
- Table storage (CRUD, basic entity operations).

## Phase 3 — Resource Manager basics

- Resource groups, fake subscriptions.
- ARM-style long-running operations so Terraform/az CLI polling works.

## Phase 4 — Compute

- Virtual networks, subnets, NICs.
- Disks, images (static catalog).
- Virtual machines (create/list/get/delete, start/stop).

## Phase 5 — Key Vault

- Secrets (CRUD).
- Keys, certificates (CRUD, basic operations).

## Phase 6 — Service Bus & Cosmos DB

- Service Bus queues/topics (send/receive).
- Cosmos DB SQL API (databases, containers, document CRUD).

## Phase 7 — Web console

- Minimal UI mirroring the gcp-emulator console, scoped to whatever
  services are implemented at that point.

Future phases will be added as the emulator's scope grows.
