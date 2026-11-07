---
name: go-check
description: Run the full quality gate of drainsim — gofmt, go build, go vet, go test and a line-ending check — and report the result in a few lines. Use before every commit, after any code change, and whenever the user asks whether the code builds or the tests pass.
allowed-tools: Bash(gofmt *) Bash(go build *) Bash(go vet *) Bash(go test *) Bash(git ls-files *) Bash(grep *)
---

# Quality gate

Run, from the repository root, and stop at the first failure to fix it:

1. `gofmt -l .` — must print nothing. Fix with `gofmt -w <files>`.
2. `go build ./...`
3. `go vet ./...`
4. `go test ./...` — `go build` does not compile `_test.go` files, so a broken
   test helper (for example `fakeSim` missing a new `Simulation` method) only
   shows here.
5. Line endings: `git ls-files --eol | grep -E "w/(crlf|mixed)"` must print
   nothing. Convert offenders to LF. `.gitattributes` must be the single
   ASCII line `* text=auto eol=lf`, not UTF-16.

If a test fails, fix the cause, not the test, unless the test itself is wrong;
say which.

Report exactly in this form:

```
gofmt ✓  build ✓  vet ✓  test ✓  eol ✓
```

with ✗ and one line of explanation for each failure.
