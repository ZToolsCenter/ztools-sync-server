# Contributing

Keep changes focused on the self-hosted authentication and synchronization service. Plugin market,
notification, analytics and SaaS administration features are intentionally outside this repository.

Before opening a pull request, run:

```bash
gofmt -w .
go vet ./...
go test ./...
```

Protocol or schema changes must include SQLite and MySQL coverage and remain compatible with the
currently released ZTools client.
