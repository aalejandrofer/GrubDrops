# Contributing

Thanks for the interest. GrubDrops is a Go + html/template/HTMX app; no
JS build step.

## Dev loop

```bash
go build ./cmd/miner
go test ./...
go vet ./...
gofmt -l .            # must be empty
```

Run locally:

```bash
MINER_MASTER_KEY=$(head -c32 /dev/urandom | base64) go run ./cmd/miner
# → http://localhost:8080
```

## Ground rules

- **UI follows `docs/DESIGN.md`** — flat: dashed-rule section labels, no panel
  borders/filled boxes, no `<table>` chrome, no zebra striping. Reuse the
  existing CSS primitives instead of inline styles.
- Templates are parsed at runtime — a stray `{{end}}` won't fail `go build`.
  The `internal/web` parse test guards this; keep it green.
- sqlc footgun: never put `?` or `(...)` inside a `queries/*.sql` comment — it
  corrupts placeholder rewriting for later queries.
- No secrets or personal infrastructure in commits.

## PRs

Keep them focused; make sure CI (build / vet / gofmt / test) is green.
