# PrepKit

A self-hostable emergency preparedness manager. PrepKit tracks preparedness
kits, the items in them, and the expiration dates of the consumables inside
them, then scores real holdings against reusable kit templates to tell you what
you are missing.

**Status: Phase 1, under construction.** Nothing here is ready to deploy yet.

This is the reference implementation of the
[PrepKit spec](https://github.com/iotemylabs/prepkit-spec). The spec repository
is authoritative for the template format, the inventory model, and the
readiness scoring algorithm; `schema/` and `templates/` here are a vendored
snapshot of it (v0.1 draft, taken 2026-07-24).

## Design constraints

- **Single static binary.** Go with `modernc.org/sqlite` (pure Go, no cgo), web
  UI embedded via `embed.FS`. Cross-compiles for `linux/arm64`; a Raspberry Pi
  is a first-class deployment target.
- **No frontend build step.** Server-rendered HTML. No JavaScript required.
- **Private by default.** Binds to localhost unless explicitly told otherwise,
  refuses to start without an admin secret, never phones home. Read
  [`docs/threat-model.md`](https://github.com/iotemylabs/prepkit-spec/blob/main/docs/threat-model.md)
  in the spec repo before deploying: a populated instance is an indexed
  inventory of household valuables and identity documents, and must be treated
  accordingly.
- **Offline export.** Generates a plain, script-free static HTML mirror
  suitable for e-ink readers and always-offline nodes.

## Running (current state)

```
PREPKIT_ADMIN_SECRET=change-me go run ./cmd/prepkit serve
```

Serves a placeholder page on `127.0.0.1:8214`. Binding any non-loopback
address requires `-allow-public-bind` and prints a warning.

## Disclaimer

PrepKit is organizational tooling, not emergency guidance. A readiness score
measures conformance to a template someone wrote, nothing more. See
[`DISCLAIMER.md`](https://github.com/iotemylabs/prepkit-spec/blob/main/DISCLAIMER.md)
in the spec repository.
