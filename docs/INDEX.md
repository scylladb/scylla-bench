# Documentation Index

Read this file at the start of any task. It indexes the standards of this
repository.

## Standards

The conventions the team decided on. Follow them when you write code. When a
standard conflicts with the task, ask the user.

### Global standards

Located in `docs/standards/global/`.

#### Coding style (`standards/global/coding-style.md`)
Format with gofumpt, goimports, and golines through `make fmt`. Put imports in
three gci groups: standard, third-party (with gocql), scylla-bench. Keep lines at 180
characters or fewer. Order struct fields for the smallest padding. Do not
shadow variables. Make sure `make check` passes
before a pull request.

#### Git workflow (`standards/global/git-workflow.md`)
Write the commit subject as `type(scope): summary` with a conventional
commit type.

### Backend standards

Located in `docs/standards/backend/`.

#### Go conventions (`standards/backend/go-conventions.md`)
Keep the `go.mod` replace directive to the ScyllaDB gocql fork. Import the
driver as `github.com/gocql/gocql`. Build with `make build`. Wrap errors with
`fmt.Errorf` and `%w`. Add no new `github.com/pkg/errors` calls. Check errors
with `errors.Is` and `errors.As`. Keep function cyclomatic complexity at 50 or
lower.

### Testing standards

Located in `docs/standards/testing/`.

#### Test writing (`standards/testing/test-writing.md`)
Add unit tests for new functions and behavior changes, with error paths. Call
`t.Parallel()` unless the test changes global state. Run tests with `-race`.
Write table-driven tests with `t.Run` subtests when a test has more than one
case. Use only the standard `testing` package. Call `t.Helper()` in test
helpers. Test ScyllaDB code against a testcontainers ScyllaDB, and remove the
containers after the test. Gate integration tests by `RUN_CONTAINER_TESTS=true`
and memory-leak tests by `RUN_MEMORY_LEAK_TEST=true`.

## Updating this documentation

- Update a standard when a team convention changes, through
  `/qatools-sdlc:standards-update`.
- Update this index when you add, remove, or change a file.
