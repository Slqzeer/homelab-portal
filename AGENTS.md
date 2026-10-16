# Agent Entry Point

Use this repository for Homelab Portal application behavior, manifests, release
evidence, and project-owned documentation.

## Reading Order

1. Read the [global knowledge conventions](https://github.com/Slqzeer/global-knowlegde/blob/main/docs/foundations/knowledge-conventions.md) before changing documentation.
2. Read [CONTEXT.md](CONTEXT.md) before naming Portal concepts.
3. Use the [documentation index](docs/README.md) to select only the relevant
   architecture, development, release, or acceptance documents.
4. Read applicable records under [docs/adr](docs/adr) before changing a durable
   boundary.
5. For issue operations, read [issue tracker](docs/agents/issue-tracker.md) and
   [triage labels](docs/agents/triage-labels.md).

## Working Rules

- This repository owns Portal code, image delivery, and deploy manifests.
  Homelab owns the Argo CD Application and shared cluster services.
- Preserve the read-only Kubernetes boundary and the distinction between catalog
  visibility and target-application authorization.
- Treat GitHub Issues as work requests and Hermes cards as execution projections.

## Completion Gate

Run `bash scripts/verify.sh source` for documentation or source changes when its
dependencies are available. Follow the release runbook for image validation and
record exact commands and observed results.
