# Runtime Technical Foundation

Updated: 2026-10-11. This document records current technology choices and delivery boundaries; module responsibilities are maintained in [Runtime Architecture](overview.md), and deployment steps are maintained in [Product Deployment Guide](../../admin/README.md).

## Current Technology Choices

| Component | Current Implementation and Entry Point |
| --- | --- |
| Backend | Single Go `molr`; version is determined by [runtime/.go-version](../../../../runtime/.go-version) |
| Client | React / TypeScript WebUI; dependencies and Node requirements are maintained in the root directory [package.json](../../../../package.json) and lock files |
| Client Interface | HTTP JSON + SSE, explicit routing; [HTTP API](../reference/http-api.md) and [TypeScript SDK](../../../../packages/sdk/README.md) |
| Module Communication | In-process Go interfaces; IM / Agent / Business and common support |
| Storage | Pure Go SQLite driver, local files, versioned domain records and persistent events |
| Execution | Independent state and serial Loop per assistant, bounded concurrency across assistants, operation receipts and checkpoint recovery |
| Models | Built-in protocol adaptation, vendor SSE and JSON responses, protected credential usage |
| Linux Shell | Bubblewrap, seccomp, cgroup v2, controlled egress and static helper |
| Release | Unified build and upgrade for core features; `CGO_ENABLED=0` static runtime artifacts |

SQLite is actually in use and is no longer a candidate storage solution. HTTP, command-line entry points, real business logic, and WebUI are all implemented; UI Design continues to serve as a mockup.

## Extension Boundaries

The backend does not load third-party business modules. New built-in tools, connection types, and model adapters are delivered by updating the Go program. Users can manage existing models, resources, authorizations, budgets, and plans.

Agents can already run scripts in approved Linux sandboxes and read project instructions and Skills from their own workspaces; this is different from dynamically installing Go modules for the Runtime. The client already has SDKs, built-in themes, and plugin theme mechanisms; administrators can import local ZIP plugin packages or add them from public HTTPS links, and authorize their activation. See [Context Updates](../runtime/agent-context.md) and [UI Package Design](../frontend/themes.md) for details.

Currently, only single-instance operation is designed; there is no multi-replica scheduling, automatic takeover across Runtimes, service discovery, or PostgreSQL adaptation. macOS uses Seatbelt and Windows uses AppContainer. Both provide controlled Shell, file tools and separately authorized Full Access, reporting actual file/network isolation and quota limits; see [Sandbox Design](../security/sandbox.md).

## Storage and Recovery Principles

An instance lock ensures that only one service uses the same data directory. Transactions save business state, events, and idempotency information; model and external tool calls do not occupy long database transactions. In-memory notifications only wake up execution, while persistent records support re-querying after restart.

Quota accounting is centralized in `internal/quota`, and the network ledger uses SQLite transactions on the actual execution side; queries utilize composite indexes by account, resource, status, and time, chat pagination uses session and sequence number indexes, and model receipt indexes only save IDs and perform direct key lookups.

SQLite uses WAL, and the local disk is managed by a single Runtime, not as a shared database for multiple servers. Attachments and the database are coordinated through staging, publishing, and recovery checks; it is not claimed that a single SQL transaction can roll back all file changes.

Key-based reads, writes, and deletions of records reuse prepared statements held by the database instance, bound separately for each transaction, with nested savepoints inheriting the current transaction; statements are released when the instance is closed. Query indexes are created within the same transaction to avoid committing index-by-index during new database initialization. `sqlitestore`'s `BenchmarkStoreOpen` and `BenchmarkRecordWrites` measure new database initialization and one thousand record writes per transaction, respectively, with the execution method being `go test ./internal/persistence/sqlitestore -run '^$' -bench 'Benchmark(StoreOpen|RecordWrites)$' -benchmem`.

Attachment objects are written, committed, and deleted serially by file ID; different objects can be transmitted simultaneously, and slow uploads do not occupy the publish lock of other objects. Operations waiting for the same object respond to context cancellation, and lock records are released after the last user exits. The publishing stage still serially verifies total storage quota, idempotent receipts, and metadata, and verifies object size and digest before commit; concurrent uploads cannot bypass quotas or create duplicate files. Object recovery only runs during startup/offline phases and does not accept uploads until completion.

During recovery, existing receipts are settled first, then local call occupations left by the previous process are terminated; unknown external results retain records and are not automatically retried. Database snapshots are not complete disaster recovery; files, workspaces, configurations, and master keys must be preserved separately. See [Backup, Recovery, and Upgrade](../../admin/deployment/maintenance.md#backup-recovery-and-upgrades) for operational steps.

The database schema version is 2. Installation resources use their own namespace; startup rejects version 1 databases without online or offline conversion. Version checks are separate from crash recovery of the current format.

## Build and Verification

`runtime/Makefile` uses `.go-version` to fix the test toolchain. Runtime artifacts require explicit static builds and checks of the final linking method and permissions; deploying the main service also requires confirming the build commit and clean state, and deployability cannot be judged solely by the success of `go build`.

`make -C runtime check` includes complete Go behavior tests and static checks; `check-contracts` only checks compilation, formatting, interface inventory, and static rules. The old "expected red light" description is no longer applicable, and the original TDD factory is bound to real implementations. See [Test Instructions](../../../../runtime/tests/README.md) for test entry points and verification boundaries.

Real kernel isolation for Shell is verified separately by [Sandbox Tests](../security/sandbox.md#7-verification). After each actual service starts, it must also pass an authentication check `/api/sandbox`; unit tests do not replace deployment capability probes.
