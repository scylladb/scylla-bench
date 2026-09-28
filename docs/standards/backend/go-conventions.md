## Go conventions

### ScyllaDB gocql fork
Keep the `replace github.com/gocql/gocql => github.com/scylladb/gocql` directive in `go.mod`.
The fork gives shard awareness. Import the driver as `github.com/gocql/gocql`.
Build with `make build`. `go install github.com/scylladb/scylla-bench@<version>` fails,
because Go does not allow a `replace` directive in a module installed by version.

### Error wrapping
Wrap an error with `fmt.Errorf` and the `%w` verb, and add context.
Do not add new calls to `github.com/pkg/errors`. Leave its current calls
unless the task changes that code.
Check errors with `errors.Is` and `errors.As`.
The errorlint linter enforces the checks.

```go
if err := session.Query(stmt).Exec(); err != nil {
	return fmt.Errorf("create keyspace %s: %w", keyspace, err)
}
```

### Function complexity
Keep the cyclomatic complexity of a function at 50 or lower. The gocyclo linter enforces the limit.
