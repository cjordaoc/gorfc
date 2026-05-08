# Contributing

This repository revives the original `gorfc` project as a community-maintained
Go binding for SAP NetWeaver RFC SDK.

## Ground Rules

- Do not commit SAP NWRFC SDK files. The SDK must be installed separately by
  each user.
- Do not commit SAP credentials, connection strings, SAProuter strings, system
  hostnames, tickets, SNC material, or customer payloads.
- Keep SDK-free tests separate from SAP-backed integration tests.
- Make unsupported behavior explicit. Silent fallback is not acceptable for
  platform, SDK, conversion, ABAP exception, or connection failures.
- Update documentation in the same change when public API, setup, validation,
  or compatibility changes.

## Development Setup

The current upstream code expects SAP NWRFC SDK headers and libraries to be
available to CGO. The revival will replace hardcoded SDK paths with portable
configuration, but until then follow the upstream README and set the relevant
CGO flags for your system.

Expected local checks for documentation-only changes:

```sh
git diff --check
```

Expected local checks for code changes when the SAP SDK is installed:

```sh
gofmt -w .
go test ./...
```

Integration tests must be opt-in and documented before they are required by CI.

## Issue And PR Shape

For fixes:

- describe the observed behavior
- describe the expected behavior
- include platform, Go version, SAP NWRFC SDK version, and SAP system release
  when relevant
- include whether the failure is build-time, connection-time, call-time, or
  conversion-time

For API changes:

- explain why the current API cannot express the behavior safely
- include a migration note
- document compatibility with upstream `gorfc`, `node-rfc`, or PyRFC where
  relevant

## Releasing (maintainer playbook)

The agent does not cut tags. The maintainer cuts a signed tag
after CI is green on `master` and the `linux-sdk-real` job has
been triggered manually with the SAP NW RFC SDK in the
self-hosted runner environment.

### v0.2.0 (and later) signed-tag procedure

1. Confirm the [Definition of Done](docs/ROADMAP_NEXUS_INTEGRATION.md#11-definition-of-done-v020)
   for the release.
2. Confirm `CHANGES` has a v0.2.0 section dated today, with
   breaking changes called out and the minimum SDK PL stated.
3. Run a final local sweep:

   ```bash
   gofmt -l .                                         # must be empty
   go vet ./...                                       # must be clean
   go vet -tags nwrfc_nosdk ./...                     # must be clean
   CGO_ENABLED=0 go test -tags nwrfc_nosdk ./...      # must be green
   CGO_ENABLED=1 go test -tags nwrfc_nosdk -race ./...# must be green
   ```

4. Confirm CI is green on `master` for `linux-nosdk`,
   `windows-nosdk`, `linux-to-windows-cross`.

5. (Optional but recommended) Trigger the manual
   `linux-sdk-real` GHA job and confirm green.

6. Sign and push the tag. The tag MUST be a signed annotated
   tag — do not use a lightweight tag for releases.

   ```bash
   git tag -s v0.2.0 -m "v0.2.0: nexus-spec integration hardening"
   git push origin v0.2.0
   ```

7. After the push, verify the tag is recognized by the Go module
   proxy:

   ```bash
   GOFLAGS=-mod=mod go install github.com/cjordaoc/gorfc/cmd/nwrfc@v0.2.0
   ```

   The first `go install` against a fresh tag may take up to a
   few minutes while the module proxy caches the version.

8. Cut a release on GitHub from the same tag. Paste the v0.2.0
   section of `CHANGES` into the release notes; cross-link to
   `docs/ROADMAP_NEXUS_INTEGRATION.md`.

9. If the release introduces a new minimum SAP NW RFC SDK PL,
   update `docs/INSTALL.md` and `docs/SDK_FUNCTIONS_MAP.md` in
   a follow-up PR (or the same PR that cut the version bump),
   and call the change out at the top of the v0.2.0 release
   notes.

The maintainer does NOT delete or re-tag a published version.
If a release is broken, cut a `v0.2.1` instead.

