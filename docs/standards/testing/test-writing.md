## Test writing

### Tests required
Add unit tests for each new function and each change in behavior.
Include error paths and edge cases.

### Parallel tests
Call `t.Parallel()` at the start of each test and each subtest.
Skip the call only when the test changes global state.

### Race detector
Run tests with the `-race` flag. `make test` sets it.

### Table-driven tests
When a test has more than one case, put the cases in a table and run each case as a `t.Run` subtest.

```go
tests := []struct {
	name string
	in   string
	want int
}{
	{name: "empty", in: "", want: 0},
}
for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		t.Parallel()
		// ...
	})
}
```

### Standard testing package
Use only the standard `testing` package. Report failures with `t.Errorf` and `t.Fatalf`.
Do not add an assertion library.

### Test helpers
Call `t.Helper()` as the first statement of a test helper. The thelper linter enforces it.

### Container tests
Test code that talks to ScyllaDB against a real ScyllaDB container from `pkg/testutil` (testcontainers).
Remove each container and resource that the test starts.
Gate each container test with an environment variable:

- Integration tests skip unless `RUN_CONTAINER_TESTS=true` is set.
- Memory-leak tests skip unless `RUN_MEMORY_LEAK_TEST=true` is set. CI sets this variable for the unit test job.
