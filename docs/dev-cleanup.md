# Developer Cleanup Tool Design

`dev-cleanup` is a production-grade cleanup CLI focused on safety, extensibility, and cross-platform behavior.

## Architecture

- Command layer: `src/cmd/dev-cleanup/main.go`
  - Flag parsing, config merge, and orchestration.
- Service layer: `src/internal/devcleanup/engine.go`
  - Planning, risk gating, execution, summary generation.
- Plugin/provider layer: `src/internal/devcleanup/providers.go`
  - `Provider` interface returns `CleanupTask` definitions.
  - Builtin provider can be complemented by more providers.
- Pattern tasks support controlled artifact cleanup (`bin/obj/dist/target`) only under explicit roots.
- IO layer: `src/internal/devcleanup/io.go`
  - Console confirmation and JSON reporting.

## Risk Model

- `safe`: low-risk cache/temp cleanup.
- `moderate`: may clear workspace metadata or browser/IDE cache.
- `aggressive`: potentially destructive commands (for example Docker prune).

Default max risk is `safe`.

## Safety Heuristics

- Refuse root-level cleanup paths (`/`, `C:\`, `.`).
- Clean directory contents, not the base folder itself.
- `min-age-hours` avoids deleting recently touched files.
- Process-aware skip for IDE/browser tasks if matching applications are currently running.
- Interactive confirmations unless `-yes=true`.
- Dry-run enabled by default.
- Command tasks are skipped when executable is not installed.

## Performance

- Parallel planning for path sizing with configurable worker count.
- Directory sizing uses streaming walk (`WalkDir`) instead of loading all entries.
- Cleanup execution is bounded and can be extended with worker pools for independent tasks.

## Cross-Platform Strategy

- `Environment` abstraction provides OS, home, temp.
- Provider decides OS-specific paths.
- Command tasks rely on command discovery (`exec.LookPath`) before execution.
- Future: split providers to files with build tags if OS specialization grows.

## Pattern Cleanup Tasks

`project-build-artifacts` is an aggressive pattern task that only runs when roots are explicitly configured via:

- CLI: `-pattern-roots "project-build-artifacts=D:/Projects|D:/Workspaces"`
- Config: `pattern_roots.project-build-artifacts`

This prevents broad accidental scans and keeps artifact deletion bounded to user-approved roots.

## Extensibility

Add a provider by implementing:

- `ID() string`
- `Tasks(env Environment) []CleanupTask`

Register provider in command composition:

- `devcleanup.NewEngine([]Provider{BuiltinProviders(...), customProvider}, ...)`

## Suggested Additional Strategies (Roadmap)

Safe:

- `go clean -modcache`
- `yarn cache clean`, `pnpm store prune`
- Thumbnail and package metadata caches
- User temp folders older than retention threshold

Moderate:

- VS Code workspace storage, extension caches
- JetBrains indexes and logs
- Browser cache/profile temp files
- Old log files and crash dumps

Aggressive:

- Docker images/volumes/build cache prune
- Virtual machine snapshots/artifacts
- Old SDK/toolchain uninstall candidates
- Project build artifacts (`bin/obj/dist/target`) discovered by pattern-based scanners

Aggressive tasks should require explicit include IDs and additional confirmation.
