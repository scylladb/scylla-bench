## Go conventions

### ScyllaDB gocql fork
Keep the `replace github.com/gocql/gocql => github.com/scylladb/gocql` directive in `go.mod`.
The fork gives shard awareness. Do not install the tool with `go install` or `go get`.
These commands ignore the `replace` directive. Build with `make build`.

### Error wrapping
Create errors with the standard library.
Wrap an error with `fmt.Errorf` and the `%w` verb, and add context.
Check errors with `errors.Is` and `errors.As`, not with `==` or a type assertion.
The errorlint linter enforces the checks.

```go
if err := session.Query(stmt).Exec(); err != nil {
	return fmt.Errorf("create keyspace %s: %w", keyspace, err)
}
```

### Function complexity
Keep the cyclomatic complexity of a function at 50 or lower. The gocyclo linter enforces the limit.
