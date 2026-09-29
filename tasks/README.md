# Tasks

One directory per Jira issue, named by the key in uppercase, as in
`tasks/ABC-123/`. Each directory holds the artifacts of the development flow:

- `intent.md`, the problem, written first.
- `spec.md`, the design. A bug fix writes `rca.md` in its place.
- `plan.md`, the files, the internals, and the tests. A bug fix may skip it.

A subtask nests under its parent, as in `tasks/ABC-123/ABC-124/`, and holds
the same artifacts. Its spec names the parent design in a `Parent` line.

The `qatools-sdlc` plugin holds the templates and writes each file through
its skills: `/qatools-sdlc:intent`, `/qatools-sdlc:spec`, `/qatools-sdlc:rca`,
`/qatools-sdlc:plan`. Commit each artifact before the stage that consumes it.
