# Contributing to FreeNet

## How to contribute

1. **Fork** the repository and create a branch from `master`:
   ```bash
   git checkout -b feature/my-feature
   ```

2. **Write tests** for any new functionality. Run the full suite before opening a PR:
   ```bash
   cd go-freenet && go test ./... && go vet ./...
   ```

3. **Build all platforms** to ensure nothing is broken:
   ```bash
   GOOS=linux GOARCH=amd64 go build ./...
   GOOS=windows GOARCH=amd64 go build ./...
   GOOS=linux GOARCH=mips GOMIPS=softfloat CGO_ENABLED=0 go build ./cmd/freenet
   ```

4. **Open a Pull Request** against `master` with a clear description of what changed and why.

## Code style

- Go: standard `gofmt` formatting, no lint warnings from `go vet`.
- Kotlin/Android: standard Android Studio formatting.
- Comments in English; user-visible strings may be Russian.

## Reporting issues

Use the GitHub Issues tab. For security vulnerabilities see [SECURITY.md](SECURITY.md).
