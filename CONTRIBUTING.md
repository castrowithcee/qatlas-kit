# Contributing

Changes follow these conventions. A rule that a test enforces is named here once; the test is the
reference.

## Design

- **Application-neutral names:** No identifier or string names an application that uses the kit.
  `architecture_test.go` enforces this. Names, prefixes, paths, keys, labels, texts and schemas are inputs.
- **No package state:** No package-level variables that change at run time and no `init()`. State lives in
  instances the application creates. Package-level variables are limited to sentinel errors, compiled
  patterns, values fixed at program start and unexported test seams.
- **Injection:** Everything an application chooses reaches the kit through parameters, option structs or
  interfaces. The kit has no defaults that belong to one application.
- **Safe defaults:** The zero value of every option is the safe choice. Security checks cannot be switched
  off. An option for a legitimate variant replaces a check with an equivalent one, never with none.
  Relaxing a check means changing the kit with a reason and tests.
- **Loud, not silent:** An unknown value, a foreign version or a damaged file is an error, never a silent
  default. If a security-relevant state cannot be read, the operation is refused.
- **Dependencies:** A new dependency needs a reason in the pull request and an exact version in `go.mod`.
- **Origin:** Code derived from another project keeps its license; its origin and any third-party license
  go into `THIRD_PARTY_NOTICES.md` in the same pull request.

## Documentation

- Each package documents its purpose, contract and security boundary in English in `doc.go`.
- Comments are English and explain only a non-obvious why.
- A new package adds one line to the package list in `README.md`.
- User documentation states the current contract only; history belongs in Git and release notes.

## Tests

- Every security check has tests of its rejection cases, not only of the success path. This covers
  signatures, archives, channels and peers, file permissions and anything else that refuses input.
- Tests run on Linux, macOS and Windows. Platform-specific code and tests use file name suffixes or
  `//go:build` constraints as in the existing packages; a platform without support returns an error that
  wraps `errors.ErrUnsupported`.
- Test data is clearly invented and contains no secrets, real hosts or personal data.
- Go files and golden files keep LF line endings (`.gitattributes`).

## Local checks

Run all checks for the whole module before pushing:

```sh
gofmt -l .                # must print nothing
go vet ./...
go test -count=1 ./...
GOOS=windows go vet ./...
GOOS=darwin go vet ./...
```

## Platforms and Go version

Supported platforms are Linux, macOS and Windows; CI runs the tests on all three. The Go version is the
`go` directive in `go.mod`, which CI uses as well.

## Commits and pull requests

- Every change reaches `main` through a pull request with green CI on all three platforms and an up-to-date
  base. Pull requests are merged by squash only.
- One pull request holds one result. A preparatory mechanical refactoring is its own pull request.
- The pull request title is the squash commit message in English
  [Conventional Commits](https://www.conventionalcommits.org) form, for example
  `feat: add release archive verification`. Title and body describe the public change.
- A breaking change before `v1.0.0` is marked with `!` in the type and described in the body, so the
  release notes can name it.
