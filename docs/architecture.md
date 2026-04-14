# Architecture Notes

This project uses a layered design:

- `src/internal/scanner`: file enumeration with filtering and error handling.
- `src/internal/hash`: chunk-based hashing with low memory overhead.
- `src/internal/duplicates`: grouping and duplicate detection logic.
- `src/internal/ui`: console prompts and progress rendering.
- `src/cmd/cleanpulse`: CLI entrypoint and orchestration.
- `src/cmd/cleanpulse-gui`: Fyne desktop wrapper for non-CLI users.

## Portability

The implementation intentionally uses only Go standard library APIs, keeping runtime behavior portable across Windows, macOS, and Linux without OS-specific dependencies.
