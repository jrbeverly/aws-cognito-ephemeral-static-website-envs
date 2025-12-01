# Devcontainer template residue

## .NET 8 SDK and Aspire features

The `.devcontainer/devcontainer.json` includes the following features:

- `ghcr.io/devcontainers/features/dotnet:2` (version 8.0)
- `ghcr.io/dotnet/aspire-devcontainer-feature/dotnetaspire:1`

And the following VSCode extensions:

- `ms-dotnettools.csdevkit`
- `ms-dotnettools.csharp`

**Status:** Template residue — not required by this project.

**Rationale:** [VISION.md](../VISION.md) §8.2 mandates a **Go** serverless backend.
The repository contains no .NET or C# source code, and no .NET-based build
targets in the Makefile. These features were inherited from a shared
devcontainer template and are not used.

**Decision:** These features are **kept as-is** for the following reasons:

1. The devcontainer is managed through a shared upstream template (see
   `.gitea/workflows/managed-devcontainer.yaml`), and local feature removal
   would be overwritten by template sync.
2. Extra tooling in the devcontainer does not affect the application build,
   runtime, or deployment.
3. Removing features risks breaking the managed workflow without providing
   meaningful benefit.

They can be safely removed if the devcontainer template is forked or
de-templated in the future.
