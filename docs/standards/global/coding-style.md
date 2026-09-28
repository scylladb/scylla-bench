## Coding style

### Formatting
Format Go code with gofumpt (with `group-params`), goimports, and golines.
Run `make fmt` before each commit. The target runs `golangci-lint run --fix`.

### Import order
Put imports in four groups, in this order:

1. The standard library.
2. Third-party modules.
3. `github.com/scylladb/scylla-bench`.
4. `github.com/scylladb/gocql`.

The gci formatter in `.golangci.yml` sets the groups.

### Line length
Keep each line at 180 characters or fewer. The lll linter and golines enforce the limit.

### Field alignment
Order struct fields so that the struct has the smallest padding.
Run `make fieldalign` to fix the order. The govet fieldalignment check enforces it.

### No shadowed variables
Do not declare a variable that shadows a variable in an outer scope.
The govet shadow check runs in strict mode.

### Lint clean
Run `make check` before you open a pull request. It must pass with no issues.
CI runs the same target.
