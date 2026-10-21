# OpenClaw plugin releases

Package: `@jogvan-k/fit-agent`; ClawHub owner: `jogvan-k`; repository:
`jogvan-k/fit-agent`; source directory: `plugin`; runtime ID: `fit-agent`.
Category: `scheduling`; topics: `fitness,intervals-icu,workouts,calendar`.

## First release (manual owner authentication)

1. Release the Go CLI protocol-1 implementation and verify the supported binary
   platforms. The package requires >=0.1.0 plus the advertised feature gate.
2. On the pinned Node/OpenClaw versions, run the validation commands in
   `plugin/README.md`, `go test -race ./...`, `go vet ./...`, and Go lint.
3. Pack and install the exact tarball in a fresh **isolated** OpenClaw state
   directory; inspect runtime registration. Run the fake-CLI onboarding tests.
4. As the owner, run `npm exec -- clawhub login` from `plugin/`; keep the
   resulting local authentication outside the repository. Review dry-run output:

   ```sh
   npm exec -- clawhub package publish . --owner jogvan-k \
     --source-repo jogvan-k/fit-agent --source-path plugin \
     --categories scheduling --topics fitness,intervals-icu,workouts,calendar --dry-run
   ```

5. Publish the same reviewed source manually with `--wait` instead of `--dry-run`.
   Wait for definitive security checks and a public listing. Record the package
   version, source commit, release URL and security-check status in release notes.
6. In another fresh isolated OpenClaw profile, install the **public package spec**
   `@jogvan-k/fit-agent@0.1.0`, inspect the six tools and test the missing-CLI
   remediation → install CLI → doctor → protected init → refresh → preview →
   confirmed sync flow using fixtures. A dry-run or linked source install is not
   evidence that the public listing works. Do not claim publication until verified.

## Trusted publishing (only after the first verified release)

Configure ClawHub's GitHub Actions trusted publisher for the exact package/owner:

- GitHub owner/repository: `jogvan-k/fit-agent`.
- Dedicated workflow: `.github/workflows/plugin-release.yml`.
- GitHub environment: `clawhub-plugin-release`.
- Release tag policy: `plugin-v*`, matching `plugin/package.json`.
- Workflow permissions: `contents: read`, `id-token: write`; no long-lived token.

Use the current pinned ClawHub `package trusted-publisher --help` / owner UI to
create that binding after public verification. Enable the dedicated release
workflow only after the registry binding is confirmed by setting repository
variable `CLAWHUB_TRUSTED_PUBLISHING=enabled`. The staged manual-dispatch workflow
checks out and validates a `plugin-vX.Y.Z` tag, uses Node 24.16+, `npm ci`, builds,
tests/typechecks/lints, validates and publishes with provenance from the tag.
Keep plugin tags separate
from Go CLI release tags. Review the OIDC issuer/audience and workflow binding in
ClawHub rather than inventing a local registry token fallback.

Dispatch **on the same release tag**, so GitHub's OIDC ref and the checked-out
source match, for example:

```sh
gh workflow run plugin-release.yml --ref plugin-v0.1.1 -f tag=plugin-v0.1.1
```

The workflow rejects a branch dispatch or mismatched package version/ref.

The implementation branch includes CI validation and an inert publisher template:
the first publication and trusted-publisher enrollment require the owner's
authenticated registry access and public security-review completion.
