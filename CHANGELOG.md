# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial release of Bankshot
- `bankshot` CLI for managing port forwards and opening URLs
- `bankshotd` daemon for handling requests
- Automatic SSH port forwarding
- URL opening in local browser from remote sessions
- Port forward management (list, add, remove)
- Daemon status monitoring
- Process wrapping with automatic port detection
- Configuration file support
- Homebrew installation support

### Features
- Cross-platform support (macOS and Linux)
- Both TCP and Unix socket communication
- Automatic cleanup of stale connections
- Real-time port monitoring for wrapped processes
- Auto-discovery of existing SSH port forwards on daemon startup

## [0.1.0] - TBD

Initial development release.

[Unreleased]: https://github.com/phinze/bankshot/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/phinze/bankshot/releases/tag/v0.1.0