# CLAUDE.md

## Commands

```bash
make check   # Lint with golangci-lint. The target installs it into bin/.
make test    # Run the unit tests with the race detector. Needs jq.
```

Integration tests run only when `RUN_CONTAINER_TESTS=true` is set.
Memory-leak tests run only when `RUN_MEMORY_LEAK_TEST=true` is set. CI sets it for `make test`.
Both test groups need Docker.

<!-- qatools-sdlc:begin -->
## Development flow

This repository uses the `qatools-sdlc` plugin. Every piece of work goes
through its flow: `/qatools-sdlc:intent <KEY>`, then `/qatools-sdlc:spec` or
`/qatools-sdlc:rca` for a bug, then `/qatools-sdlc:plan`, then the code.
Commit each artifact before the stage that consumes it. A review works
through `/qatools-sdlc:review`. File a Jira issue about our work with
`/qatools-sdlc:issue`. It needs the Atlassian connector.
The user may skip the flow for a very small fix when they say so. The pull
request description then states the skip in one line.

If the `/qatools-sdlc:*` skills are not available, stop and ask the user to
run these two commands, then start a new session:

    /plugin marketplace add git@github.com:scylladb/qatools.git
    /plugin install qatools-sdlc@qatools

Jira keys: `QATOOLS-<n>`. Task artifacts: `tasks/<KEY>/`, or
`tasks/<PARENT>/<KEY>/` for a subtask. Read `docs/INDEX.md` before any task
and follow the standards in `docs/standards/`. Suggest
`/qatools-sdlc:standards-update` when a convention comes up that no standard
holds. Verify sequence: section `Commands` of this file.
<!-- qatools-sdlc:end -->
