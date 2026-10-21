# Contributing to tt-feature-discovery

Thank you for your interest in contributing.

## Reporting bugs

Report bugs and request features through [GitHub Issues](https://github.com/tenstorrent/tt-feature-discovery/issues).
Include the chart or image version, Kubernetes and node-feature-discovery versions, and relevant logs.

Do not report security vulnerabilities through public issues. See [SECURITY.md](./SECURITY.md).

## Submitting changes

Bug fixes and new functionality are submitted as pull requests. Pull requests are reviewed weekly by default.

1. Fork the repository and create a branch from `main`.
2. Make your change, with tests for new or changed behavior.
3. Run `make vet test lint` and ensure all checks pass.
4. If you change `proto/orchestrator.proto`, run `make proto` and commit the regenerated bindings.
5. If you change Helm chart values, regenerate the chart README (see the `helm-docs` workflow).
6. Open a pull request describing the change and its motivation.

Pull requests are squash-merged and require at least one approving review.

## Coding standards

- Go code must pass `go vet` and `golangci-lint` (see `.golangci.yml`).
- Every new source file must start with SPDX headers:

  ```
  // SPDX-License-Identifier: Apache-2.0
  // SPDX-FileCopyrightText: <year> Tenstorrent USA, Inc.
  ```

  Use the comment syntax appropriate to the file type.

## Code of Conduct

This project follows the [Contributor Covenant](./CODE_OF_CONDUCT.md). By participating you agree to abide by it.

## License

By contributing, you agree that your contributions are licensed under the project's [Apache 2.0 license](./LICENSE).
