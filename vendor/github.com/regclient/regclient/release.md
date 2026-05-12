# Release v0.11.4

Security:

- Validate server URL in token auth. ([PR 1075][pr-1075])
- Upgrading Go fixes CVE-2026-33814  and CVE-2026-39836, other vulnerabilities fixed in 1.26.3 were not called by this project. ([PR 1084][pr-1084])

Features:

- Support scanning OCI Layout for referrers. ([PR 1074][pr-1074])
- Add created timestamp in OCI Layout entries. ([PR 1081][pr-1081])
- `tag.ls` now accepts the same pagination parameters as `repo.ls`. ([PR 1086][pr-1086])

Fixes:

- Push tags for minor and major releases on Docker Hub. ([PR 1087][pr-1087])

Contributors:

- @ffried
- @sudo-bmitch

[pr-1074]: https://github.com/regclient/regclient/pull/1074
[pr-1075]: https://github.com/regclient/regclient/pull/1075
[pr-1081]: https://github.com/regclient/regclient/pull/1081
[pr-1084]: https://github.com/regclient/regclient/pull/1084
[pr-1086]: https://github.com/regclient/regclient/pull/1086
[pr-1087]: https://github.com/regclient/regclient/pull/1087
