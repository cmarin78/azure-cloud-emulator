# Contributing

This project mirrors [gcp-emulator](../gcp-emulator)'s conventions, so if
you're already familiar with that codebase most of this will feel
familiar.

## Project layout

- `internal/services/<name>` — one Go package per Azure service, each
  with its own `Register(mux *http.ServeMux)`. ARM control-plane
  resources follow the standard
  `/subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}`
  shape; data-plane (blob/queue/table/etc.) services are wired through
  the shared dispatcher in `registerDataPlane`
  (`cmd/azure-emulator/main.go`) rather than registering their own
  `mux.HandleFunc`.
- `internal/server` — shared ARM request/response helpers
  (`RequireAPIVersion`, error shaping, async-operation polling) used by
  every service package.
- `internal/storage` — the BoltDB wrapper every service uses for
  persistence (`Put`/`Get`/`Delete`/`List` against a named bucket).
- See `ROADMAP.md` for the phase-by-phase history of what's implemented
  and what's proposed next.

## Adding or changing a service

1. Add/modify the package under `internal/services/<name>`, following an
   existing package (`storageaccounts` or `keyvault` are good
   reference points) for the CRUD/async/error-shape conventions.
2. Wire it into `cmd/azure-emulator/main.go` and register its provider
   namespace in `internal/services/resourcemanager/registeredNamespaces`
   (including `apiVersions` — `azurerm`'s cleanup logic needs them; see
   `docs/poc-terraform-azurerm.md` for why this matters in practice).
3. Add a `<name>_test.go` covering ARM CRUD and any data-plane behavior
   via `httptest` (no real network, no real Docker).
4. Extend the `az rest` smoke tests
   (`scripts/test-az-cli.sh`/`scripts/test-az-cli.ps1`) and, where it
   makes sense, a Terraform smoke test (`terraform/smoke-test/` for the
   generic `http` provider, `terraform/azurerm-smoke-test/` for the real
   `azurerm` provider) — live verification against a running instance,
   not just unit tests, is the standard this project holds itself to.
5. Update `README.md`/`ROADMAP.md` for the new phase or change.

## Before opening a PR

```bash
go build ./...
go vet ./...
gofmt -l .          # should print nothing
go test ./... -race
```

Never commit build artifacts, the local BoltDB file, or Terraform
state — they're all gitignored already (`*.exe`, `*.db`,
`.azure-emulator-data/`, `terraform.tfstate*`). CI fails the build if
any of these end up tracked.

## Commit style

Plain, descriptive commit messages scoped to one phase or fix at a
time (see `git log` for examples) — no strict conventional-commits
format is enforced, but keep unrelated changes out of the same commit.

## Reporting a security issue

See `SECURITY.md`.
