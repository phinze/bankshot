# Contributing to Bankshot

Thank you for your interest in contributing to Bankshot! This document provides guidelines and information for contributors.

## Development Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/phinze/bankshot.git
   cd bankshot
   ```

2. Install Go 1.21 or later

3. Build the project:
   ```bash
   make build
   ```

4. Run tests:
   ```bash
   make test
   ```

## Versioning Strategy

Bankshot follows [Semantic Versioning](https://semver.org/) (SemVer):

- **MAJOR** version: Incompatible API changes
- **MINOR** version: New functionality in a backwards compatible manner
- **PATCH** version: Backwards compatible bug fixes

### Version Format

- Release versions: `v1.0.0`, `v1.2.3`
- Pre-release versions: `v1.0.0-beta.1`, `v1.0.0-rc.1`
- Development builds: `dev` (from source)

### Creating a Release

1. Update CHANGELOG.md with release notes
2. Create and push a tag:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```
3. The GitHub Actions workflow will automatically:
   - Build binaries for all platforms
   - Create a GitHub release
   - Update the Homebrew formula

## Code Style

- Follow standard Go conventions
- Run `make lint` before submitting PRs
- Keep code simple and well-documented

## Commit Messages

We recommend following conventional commit format:

- `feat:` New features
- `fix:` Bug fixes
- `docs:` Documentation changes
- `test:` Test additions or changes
- `refactor:` Code refactoring
- `chore:` Maintenance tasks

Example:
```
feat: add support for custom SSH commands
fix: handle connection timeouts gracefully
docs: update installation instructions
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/amazing-feature`)
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass (`make test`)
6. Run linter (`make lint`)
7. Commit your changes
8. Push to your fork
9. Open a Pull Request

## Testing

- Write tests for new features
- Ensure existing tests pass
- Include both unit and integration tests where appropriate

## Documentation

- Update README.md for user-facing changes
- Add inline documentation for exported functions
- Update configuration examples if needed

## Questions?

Feel free to open an issue for:
- Bug reports
- Feature requests
- Questions about the codebase

Thank you for contributing!