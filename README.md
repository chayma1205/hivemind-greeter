# hivemind-greeter

A small Go web service that greets the caller, built and shipped by this
repo's own CI/CD pipeline. This repo owns **the application**: source,
Dockerfile, tests, and the pipeline that builds, scans, signs, and pushes
its image to ECR.

Everything downstream of "image landed in ECR" — the Kubernetes cluster,
Helm charts, Argo CD, TLS, DNS — lives in a separate repo,
[`hivemind-challenge`](https://github.com/chayma1205/hivemind-challenge),
which pulls new releases in via Argo CD Image Updater rather than this
repo ever touching the cluster directly. See that repo's
`docs/DECISIONS.md` #11 for why the split.

> `app/README.md` is the original challenge brief this project was built
> against — kept as-is for reference, not day-to-day documentation. This
> file is the latter.

## The application

[`app/greeter.go`](app/greeter.go) — a single-file, dependency-free Go
HTTP server:

* Logs a startup line and the value of the `HELLO_TAG` environment
  variable, then listens on `:8080`.
* `GET /` responds `Hello, <caller-IP>! I'm <hostname>, running tag
  <tag>` — the caller's IP (from `X-Forwarded-For` if present, otherwise
  `RemoteAddr`), the pod's own hostname (so it's obvious which replica
  answered a given request behind a load balancer), and the running tag.
* The tag is `HELLO_TAG` (set at deploy time) unless overridden per
  request via a `?tag=` query parameter — the URL-parameter requirement
  from the original challenge brief (`app/README.md`), closed after
  being env-var-only for a while (see `docs/DECISIONS.md`/`ASSESSMENT.md`
  in [hivemind-challenge](https://github.com/chayma1205/hivemind-challenge)
  for the history of this being tracked as a gap).

### Build and run locally

```bash
cd app
go build -o greeter .
HELLO_TAG=local ./greeter
# curl http://localhost:8080/              -> tag from HELLO_TAG
# curl http://localhost:8080/?tag=override -> tag from the query param
```

No `go.sum` — zero non-stdlib dependencies, so there's nothing for a
module cache to key off of (`ci.yml` disables Go's module cache for
exactly this reason).

### Build the container image locally

```bash
cd app
docker build -t hivemind-greeter:local .
docker run -p 8080:8080 -e HELLO_TAG=local hivemind-greeter:local
```

Multi-stage build ([`app/Dockerfile`](app/Dockerfile)): `golang:1.25-alpine`
compiles a static binary (`CGO_ENABLED=0`), then a `distroless/static`
final image copies in just the binary — no shell, no package manager, no
Go toolchain in the shipped image. Runs as `nonroot:nonroot`, not root.

## CI/CD pipeline

Five workflows, each with one job:

| Workflow | Trigger | Does |
|---|---|---|
| [`ci.yml`](.github/workflows/ci.yml) | PR to `main`; push to `main`/`releases/**` (paths: `app/**`) | `gofmt`/`vet`/`build`/`test`, then builds the image (not pushed) and Trivy-scans it. No AWS credentials — this workflow never touches a registry. |
| [`cd.yml`](.github/workflows/cd.yml) | `ci.yml` succeeding on `main` | Builds, scans, signs, and pushes to ECR tagged with the commit's short SHA. |
| [`cd-staging.yml`](.github/workflows/cd-staging.yml) | `ci.yml` succeeding on a `releases/vX.Y[.Z]` branch | Same, tagged `vX.Y[.Z]-<short-sha>`. No staging cluster exists to deploy this to yet — the tag shape can never match prod's Image Updater regex (the `-<sha>` suffix breaks the `^vX.Y.Z$` match), so it can't reach prod by accident. |
| [`cd-release.yml`](.github/workflows/cd-release.yml) | A GitHub Release is published | Validates the tag is `vX.Y.Z` exactly, then builds/tests/scans/pushes an image under that tag. **This is the only path that reaches prod** — Argo CD Image Updater in `hivemind-challenge` watches ECR for tags matching this shape and writes the new tag into that repo's git, which `selfHeal` then deploys. |
| [`_build-push.yml`](.github/workflows/_build-push.yml) | Called by the three above (`workflow_call`) | The actual build/scan/sign/push logic, shared once so `cd.yml`/`cd-staging.yml`/`cd-release.yml` only differ in trigger, tag derivation, and whether they also run tests (`cd-release.yml` does, since it has no prior `ci.yml` run on the exact ref to depend on). |

**Never un-gated by CI**: `cd.yml`/`cd-staging.yml` trigger on `ci.yml`
*succeeding*, not on the same push event — a red `ci.yml` run can't be
raced by a push to prod. `cd-release.yml` is the one exception (a
Release can be cut from any ref, with no fixed prior CI run to point
at), so it runs the Go lint/test steps itself first.

**Prod only moves on a published Release**, not every merge to `main` —
a deliberate promotion gate (see `hivemind-challenge`'s
`docs/DECISIONS.md` #11). `main`/`releases/**` pushes still produce real,
scanned, signed images in ECR (commit-SHA / staging tags) without ever
reaching prod.

### Supply chain security

Every pushed image, regardless of which CD workflow pushed it, gets:

* **Trivy scan** (CRITICAL/HIGH, fails the build on an unfixed finding —
  `ci.yml`'s build-time scan; `_build-push.yml` re-scans before push
  too), with SARIF results uploaded to the repo's Security tab.
* **Keyless cosign signature** — the signing identity is the workflow's
  own GitHub OIDC token, recorded in the public Rekor transparency log.
  No signing key exists to generate, store, or rotate.
* **SBOM** (SPDX, via Trivy) attested to the image with cosign.
* **SLSA v1 provenance attestation** — records the exact builder
  (this workflow, by path), the source commit, the triggering event, and
  the specific Actions run/attempt that produced the image.

Signatures/attestations live in a **separate** ECR repository
(`hivemind-greeter-signatures`, mutable tags) from the image itself
(`hivemind-greeter`, immutable tags) — cosign rewrites `.sig`/`.att` tags
in place as new attestations are added, which an immutable-tag policy
would reject.

### AWS access

All three CD workflows authenticate to AWS via GitHub OIDC
(`aws-actions/configure-aws-credentials`, `id-token: write`) — no static
AWS access keys anywhere in this repo. The role assumed is
`hivemind-prod-github-actions-ecr-push`, provisioned in
`hivemind-challenge`'s `terraform/envs/prod/github.tf`, trusted only for
this repo's OIDC subject claims.

> **⚠️ Known-broken as written**: [`_build-push.yml`](.github/workflows/_build-push.yml)
> hardcodes `AWS_ROLE_ARN:
> arn:aws:iam::130237968586:role/hivemind-prod-github-actions-ecr-push`.
> The actual account is `324464328617` — confirmed live via
> `aws sts get-caller-identity` and `aws iam get-role
> --role-name hivemind-prod-github-actions-ecr-push`, whose real ARN is
> `arn:aws:iam::324464328617:role/hivemind-prod-github-actions-ecr-push`.
> As written, every CD run's OIDC `AssumeRoleWithWebIdentity` call would
> fail against a nonexistent account, meaning **the CD pipeline as
> currently committed doesn't work**. `ci.yml` (no AWS access) is
> unaffected. This is very likely why the deployed image in prod today
> was pushed manually rather than by this pipeline. Fix: update the
> `AWS_ROLE_ARN` value in `_build-push.yml` to the correct account ID.

## Versioning

Prod images are tagged `vX.Y.Z` (semver, matching the GitHub Release tag
exactly — enforced by `cd-release.yml`'s tag-format check). Non-prod
images use commit-SHA (`main`) or `vX.Y[.Z]-<sha>` (`releases/**`)
tags, both immutable in ECR (`repository_image_tag_mutability =
"IMMUTABLE"` in `hivemind-challenge`'s Terraform) so a tag always points
at exactly one build.

## Related repo

Infrastructure, Kubernetes manifests, and the GitOps pipeline that
actually runs this app live in
[`hivemind-challenge`](https://github.com/chayma1205/hivemind-challenge).
That repo's `charts/env/prod/apps/greeter` is the Helm chart; its
`docs/ARCHITECTURE.md` and `docs/RUNBOOK.md` cover the running system
end to end.
