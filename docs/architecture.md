# Architecture Notes

This project uses a layered design:

- `src/internal/scanner`: file enumeration with filtering and error handling.
- `src/internal/hash`: chunk-based hashing with low memory overhead.
- `src/internal/duplicates`: grouping and duplicate detection logic.
- `src/internal/ui`: console prompts and progress rendering.
- `src/cmd/duplica-scan`: CLI entrypoint and orchestration.
