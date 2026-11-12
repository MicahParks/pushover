# Contributing

Bug reports, fixes, and improvements are welcome. For larger changes, please open an issue to discuss the approach
before sending a pull request.

## Development

This module has no dependencies outside the Go standard library. Please keep it that way.

Before sending a pull request, run:

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

The behavior of this package follows the [Pushover API documentation](https://pushover.net/api). When changing how a
request is encoded or validated, link to the relevant section of the documentation in the pull request.

## Live tests

Live tests against the real Pushover API run when `PUSHOVER_TOKEN` and `PUSHOVER_USER` are set:

```sh
PUSHOVER_TOKEN=... PUSHOVER_USER=... go test -run Live -v
```

Live tests must only send `PriorityLowest` messages with a short `TTL`, so they don't notify anyone, and must not
change the account, for example by creating groups or assigning licenses. Use a fake server for everything else, see
`helpers_test.go`.
