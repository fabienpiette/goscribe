# Task Completion Checklist

When finishing any code change in goscribe, do the following in order:

1. **Format**: `go fmt ./...`
2. **Test**: `make test` (or `make test-short` for quick check)
3. **Lint**: `make lint`
4. **Check invariants** — verify no new import crosses these boundaries:
   - `pkg/config` must not import anything from `internal/`
   - `internal/util` must not import any project package
   - Only `internal/provider` imports both `internal/openai` and `internal/gemini`
   - `internal/api` must not import `internal/provider`, `internal/openai`, or `internal/gemini`
5. **Commit**: Conventional Commits format — `<type>[scope]: <description>`, lowercase imperative, max 50 chars, no trailing period, no AI attribution
