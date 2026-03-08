# Server Audit Log

`deck serve` writes audit records to a JSONL log file under the bundle root.

## Location

Default file location:

```text
<root>/.deck/logs/server-audit.log
```

## Standard fields

Each normalized record uses these common fields:

- `ts`: RFC3339Nano timestamp in UTC
- `schema_version`: currently `1`
- `source`: log producer, typically `server`
- `event_type`: event name such as `http_request` or `registry_seed`
- `level`: `debug`, `info`, `warn`, or `error`
- `message`: short human-readable description

Optional job-related fields appear when relevant:

- `job_id`
- `job_type`
- `attempt`
- `max_attempts`
- `status`
- `hostname`

Non-standard details are nested under `extra`.

## Typical examples

- HTTP request records carry request fields under `extra`
- Registry seeding records describe archive and target details under `extra`
- Lifecycle-style records keep decision metadata under `extra.decision`

## Viewing logs

```bash
deck logs --source file --path <root>/.deck/logs/server-audit.log
```
