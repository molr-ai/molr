# Continuous plans and the watcher protocol

An `assistant_watch` plan appears in the continuous-watch group in global and assistant plan lists, without calendar occurrences. The assistant prepares and validates the program; Runtime does not adapt websites or third-party CLIs. See [Plans and execution](../../user/guides/plans.md).

## Creation and management

Call `plan_manage` with `action=create`, `mode=assistant_watch`, `title`, `instructions`, and `watcher`. Watcher configuration requires `compute_id` and `script`; optional fields are `sandbox_id`, `directory`, `files`, `network_mode`, `network_ids`, and `health_seconds`. File and network references follow the Shell resource rules. TTY, port forwarding, Full Access, and Shell timeout parameters are not accepted. Watcher stdin currently supports Linux and macOS; Windows backends return an explicit capability error.

Set `start_at` or `end_at` only when requested. An indefinite plan does not grant indefinite permission. Creation registers a plan for asynchronous startup; report readiness only after it reaches `running`. `plan_read` exposes configuration, checkpoint, recent events, and failure reason. `update` changes configuration or `paused`; `execute` requests resumption. Preserve plans with history by pausing them. Watches do not accept recurrence rules, calendar occurrences, or dependency handoffs.

Ordinary Shell timeout, background reminder, and TTY behavior remain unchanged. Watchers use existing managed execution and concurrency limits, without ordinary Shell running reminders or completion events. Manage watcher lifecycle through its plan; Shell controls may only read its logs.

Changing watcher configuration increments the plan revision, stops the old execution, and clears its checkpoint. Instruction or date edits preserve the checkpoint but restart observation. Referenced script files are read at execution time; use versioned paths to pin scripts and dependencies. A configuration snapshot is not a snapshot of the dependency environment.

## Standard input and output

Messages are newline-terminated UTF-8 JSON objects. Reserve stdout for the protocol and stderr for logs. Read this startup message from stdin:

```json
{"type":"start","version":1,"state":null,"resumed":false}
```

`state` contains the last acknowledged checkpoint, initially null. `resumed` indicates an observation interruption; the program must verify whether the source supports catching up instead of assuming continuous coverage.

| Type | Fields | Meaning |
| --- | --- | --- |
| `ready` | `version: 1` | Initialization complete; required within 30 seconds |
| `checkpoint` | `state` | Successful observation or verified subscription health; saves progress without calling a model |
| `event` | `key`, `data`, optional `state` | A business change for the assistant |
| `complete` | Optional `state` | Observation objective achieved; accepted events remain pending for processing |

Example:

```jsonl
{"type":"ready","version":1}
{"type":"checkpoint","state":{"cursor":123}}
{"type":"event","key":"issue-124-opened","data":{"number":124,"title":"Example issue"},"state":{"cursor":124}}
```

After each successful durable write, stdin receives:

```json
{"type":"ack","for":"event","key":"issue-124-opened"}
```

Send messages sequentially and consume acknowledgements. A successful print is not a persistence acknowledgement. A key identifies an immutable business event within its plan. Retrying the same key and data does not enqueue another event or overwrite a newer checkpoint; conflicting data is a protocol error. An event and its checkpoint commit together. Successful checks without changes should save a checkpoint.

Unknown types, extra fields, unsupported versions, invalid JSON, missing fields, non-JSON stdout, and oversized messages fail the protocol. Do not emit further messages after `complete`. Exiting without `complete` is an unexpected termination.

Limits: 64 KiB per line, 4 KiB state, 8 KiB data, 256-byte keys, and 32 messages per second. Pending events are bounded to 128 entries and 256 KiB of event data; overflow fails explicitly without discarding acknowledged state. Details retain the latest 100 events; deduplication records persist separately. Logs remain subject to execution output and storage limits.

## Model processing and health

Unchanged observations do not produce model events. Runtime freezes up to eight changes with the plan's instruction snapshot into an assistant task. While that plan has an unfinished processing task, further changes remain queued. Assistant permissions and budget still apply. Processing failures do not rerun the watcher or mutate a frozen batch.

An optional `health_seconds` deadline requires meaningful protocol progress. Zero or omission only monitors process status. Submit a checkpoint after successful checks even when business data is unchanged. An unconditional heartbeat thread does not prove observation health. Startup and health deadline failures interrupt observation.

## Authorization, failure, and recovery

Existing assistant compute, file, and network authorization applies at startup and during execution. Expiry or revocation stops the watcher and leaves the plan blocked. Runtime emits one failure notice and assistant event, allowing the assistant to explain or request the necessary permissions. Changed grants are rechecked; paused or expired plans do not start. Reaching the authorized maximum execution duration also blocks the plan rather than restarting to bypass the limit.

Transient faults receive bounded backoff; three consecutive failures block the plan. Protocol errors, queue overflow, and unsupported platforms do not retry automatically. Resume the plan after fixing the problem. After Runtime restart, checkpoints survive and recovery waits for old remote execution leases to expire before starting again. Watchers must resume observation from acknowledged state; perform business side effects in event processing instead.

Tests cover protocol validation, queue limits, transaction rollback, revision fencing, list/calendar separation, immutable event batches, real sandbox communication, and grant expiry:

```bash
CGO_ENABLED=0 go test ./internal/business/watch ./internal/business/plan
bash scripts/test-sandbox-linux.sh -run TestWatcherReal -count=1 -v
```
