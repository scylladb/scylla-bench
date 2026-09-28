## Coding style

### Formatting
Format Go code with gofumpt (with `group-params`), goimports, and golines.
Run `make fmt` before each commit. The target runs `golangci-lint run --fix`.

### Import order
Put imports in three groups, in this order:

1. The standard library.
2. Third-party modules. This group includes `github.com/gocql/gocql`.
3. `github.com/scylladb/scylla-bench`.

The gci formatter in `.golangci.yml` sets the groups. Run `make fmt` to fix the order.

### Line length
Keep each line at 180 characters or fewer. The lll linter and golines enforce the limit.

### Field alignment
Order struct fields so that the struct has the smallest padding.
The govet fieldalignment check in `make check` enforces it.

### No shadowed variables
Do not declare a variable that shadows a variable in an outer scope.
The govet shadow check runs in strict mode.

### Lint clean
Run `make check` before you open a pull request. It must pass with no issues.
CI runs the same target.
