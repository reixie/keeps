# Contributing to Keeps

For bugs and feature requests, open an issue in this repository. Before starting
a substantial change, describe the intended behavior in an issue so its scope
can be discussed.

## Local development

Follow the requirements and setup instructions in [README.md](README.md).
Use a separate library while developing changes that write metadata or remove
captures:

```sh
KEEPS_LIBRARY=/tmp/keeps-dev-library ./script/build_and_run.sh
```

`script/build_native.sh` compiles the Swift adapters. Run it after Swift changes
and before `go tool mygo dev` or `go tool mygo build`.

## Conventions

- Format Go code with `gofmt` and module files with `go mod edit -fmt`.
- Follow the existing four-space indentation in Swift files.
- Keep UI state on the main thread and file or network work off it.
- Route library mutations through the existing serial queue and metadata lock.
- Preserve imported originals, stable capture IDs, and library-format compatibility.
- Keep changes focused and update the documentation when behavior or setup changes.
- Commit `go.mod` and `go.sum` together when changing dependencies. Exclude generated
  binaries, local libraries, credentials, and editor settings.

## Before submitting a pull request

```sh
gofmt -l .
go vet ./...
go mod verify
./script/build_native.sh
go tool mygo build -platform "darwin/$(go env GOARCH)" -skip-dmg
```

`gofmt -l .` should produce no output. Open the resulting app and review the
behavior affected by your change using a separate library. For UI changes,
include screenshots and check keyboard navigation, resizing, and both system
appearances where relevant.

Describe the problem, the resulting behavior, and the commands or manual steps
you used to review the change. Include your macOS version and architecture for
platform-specific issues.
