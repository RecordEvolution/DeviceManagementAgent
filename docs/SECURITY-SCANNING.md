# Security / CVE screening

How we screen the agent for known vulnerabilities (CVEs) in our code and all
dependencies.

## What is screened (and what isn't)

The production artifact is the **bare, statically-linked Go binary** (`CGO_ENABLED=0`)
that runs directly on each edge device's host OS. So the screen targets exactly
what ships:

- ✅ **Our Go code + all module dependencies** — `go.mod` closure, including the
  `RecordEvolution/nexus` fork pulled in via `replace`.
- ✅ **The embedded `frpc` binary** — a third-party Go binary (`src/embedded/frpc_binary`,
  pinned by `FRP_VERSION`) compiled into the agent. It is **not** in `go.mod`, so a
  module scan alone misses it.

Out of scope (deliberately):

- ❌ **The Docker images** — the builder (`Dockerfile`) is a throwaway build vehicle;
  the `ubuntu:22.04` runtime image (`docker/Dockerfile`) is only used for local dev
  against the REDeployments stack. Neither ships to devices.
- ❌ **The edge host OS** — it's the customer's device, not something we ship.

## Tools

- **[`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)** (official
  Go vuln scanner) — does **reachability analysis**, so it only flags vulnerabilities
  your code can actually reach (low false-positive rate). It builds the code, so it
  honours the `replace` directive.
  - *source mode* (`./...`) — our code + all module deps.
  - *binary mode* (`-mode=binary`) — scans a compiled Go binary via its embedded
    build info; used for the **frpc** binary.
- **[`cyclonedx-gomod`](https://github.com/CycloneDX/cyclonedx-gomod)** — Go-native
  CycloneDX SBOM generator (module graph + binary).

Both run via `go run` and are version-pinned in the `Justfile` (`GOVULNCHECK_VERSION`,
`CYCLONEDX_GOMOD_VERSION`). No Docker, no other host tooling required.

## Running it

```bash
just security      # everything below, report-only (never fails)

just vuln-go       # our Go code + module deps (exit 3 = reachable vulns found)
just vuln-frpc     # the embedded frpc binary (frp itself + its bundled deps)
just sbom          # CycloneDX SBOMs -> build/sbom/{reagent,frpc}-*.cdx.json
just sarif         # govulncheck SARIF -> build/sarif/ (uploaded to Security tab by CI)
just vuln-binaries # optional: scan each cross-compiled build/reagent-* artifact
```

`govulncheck` exits **3** when it finds reachable vulnerabilities — that's expected
while we're report-only. `just security` wraps the scans and always exits 0.

## CI

`.github/workflows/security.yml` runs the scans on every PR, on push to `master`,
and **weekly** (`schedule`) — the weekly run matters because CVEs are disclosed
against code long after it's written, so a frozen codebase still needs periodic
re-scanning. It is **report-only**: nothing here fails the build.

- **Code scanning (Security tab):** `just sarif` emits govulncheck SARIF for the
  code and the frpc binary, which is uploaded via `github/codeql-action/upload-sarif`
  under two categories (`govulncheck-code`, `govulncheck-frpc`). Findings then show
  up under the repo's **Security → Code scanning** tab.
- **Fallbacks:** the job also prints a finding summary to the log and uploads the
  raw SARIF + the CycloneDX SBOMs as workflow artifacts.

> The Security-tab upload needs Code scanning enabled — i.e. a public repo or, for a
> private repo, **GitHub Advanced Security**. Without it the upload steps no-op
> (`continue-on-error`) and you rely on the log summary + the SARIF/SBOM artifacts.

## Signed SBOM attestation (release provenance)

The **release** workflow (`.github/workflows/release.yml`, tag `vX.Y.Z` / manual) attests
**the exact binaries it publishes**. For every target it builds the binary, generates a
per-binary CycloneDX SBOM (`just sbom-bin`), records a **sigstore-signed SBOM attestation**
bound to the binary's digest (`actions/attest-sbom`), and then publishes that same binary to
`gs://re-agent`. No GHAS required. Verify any published binary with:

```bash
gh attestation verify reagent-linux-amd64 --repo RecordEvolution/DeviceManagementAgent
```

Because CI both attests and publishes the same bytes, the attestation matches what ships —
as long as releases go through CI (tag push) rather than a local `just rollout`.

## Remediation status

- **Go standard-library CVEs** (the bulk of our findings — `crypto/tls`, `crypto/x509`,
  `net/url`, `net/http`, …) are fixed by the **Go toolchain**. We pin **`toolchain go1.26.4`**
  in `src/go.mod` (and `golang:1.26-alpine` in the builder `Dockerfile`); CI's `setup-go`
  reads `go.mod`. This cleared all 14 reachable stdlib CVEs. Bump the toolchain as new ones
  are disclosed.
- **frpc binary CVEs** — remediated by bumping **`FRP_VERSION`** to a newer release (kept in
  sync across `Justfile`, `scripts/build.sh`, and `src/embedded/frpc.go`).

### Accepted findings (not actionable)

`govulncheck` reports two CVEs in `github.com/docker/docker` as reachable; they are
**accepted / not applicable**:

- **GO-2026-4887** (AuthZ-plugin bypass) and **GO-2026-4883** (plugin-privilege off-by-one)
  are **daemon-side** Moby issues. reagent is a Docker **client** — it manages containers and
  never runs an authorization plugin or installs plugins, so the vulnerable code paths are not
  exercised. govulncheck flags them only because importing the client SDK pulls the module's
  `init()` chain into the call graph.
- There is **no fix in `github.com/docker/docker`** (all versions affected, incl. v28.x); the
  fix lives only in the renamed, still-**beta** module `github.com/moby/moby/v2@2.0.0-beta.8`.
  Migrating a production agent to a beta Docker SDK isn't warranted for a non-applicable issue.

Revisit when `github.com/moby/moby/v2` reaches a stable release (then migrate the SDK import).

## Roadmap: report-only → gating

We start **report-only** to establish a clean baseline without blocking everyone.
Once the baseline is addressed, tighten by failing on fixable findings — e.g. drop
`continue-on-error` in CI, or add `-mode=binary` gating, and track accepted/unfixable
items in a suppression file. (Dependabot auto-update PRs are a possible follow-up.)
