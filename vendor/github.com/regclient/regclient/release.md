# Release v0.10.0

Features:

- Feat: Support DOCKER_AUTH_CONFIG variable. ([PR 996][pr-996])
- Feat: Add regctl repo copy. ([PR 997][pr-997])
- Feat: regsync support for semantic versioning(semver) for matching tags ([PR 1005][pr-1005])
- Feat: Add `tagSets` to regsync. ([PR 1008][pr-1008])

Changes:

- Chore: Add go:fix lines to deprecated code. ([PR 994][pr-994])
- Chore: Add gofumpt to the build. ([PR 995][pr-995])
- Chore: Remove the unused bps field. ([PR 998][pr-998])
- Fix: Handle semver compare of numeric prerelease ([PR 1007][pr-1007])

Security:

- CVE-2025-58187: Fixed with Go upgrade (<https://osv.dev/GO-2025-4007>).
- CVE-2025-58189: Fixed with Go upgrade (<https://osv.dev/GO-2025-4008>).
- CVE-2025-61723: Fixed with Go upgrade (<https://osv.dev/GO-2025-4009>).
- CVE-2025-47912: Fixed with Go upgrade (<https://osv.dev/GO-2025-4010>).
- CVE-2025-58185: Fixed with Go upgrade (<https://osv.dev/GO-2025-4011>).
- CVE-2025-58186: Fixed with Go upgrade (<https://osv.dev/GO-2025-4012>).
- CVE-2025-58188: Fixed with Go upgrade (<https://osv.dev/GO-2025-4013>).
- CVE-2025-58183: Fixed with Go upgrade (<https://osv.dev/GO-2025-4014>).
- CVE-2025-9230: Fixed with Alpine image upgrade.
- CVE-2025-9230: Fixed with Alpine image upgrade.
- CVE-2025-9232: Fixed with Alpine image upgrade.
- CVE-2025-9232: Fixed with Alpine image upgrade.
- CVE-2025-9231: Fixed with Alpine image upgrade.
- CVE-2025-9231: Fixed with Alpine image upgrade.

Contributors:

- @daimoniac
- @sudo-bmitch

[pr-994]: https://github.com/regclient/regclient/pull/994
[pr-995]: https://github.com/regclient/regclient/pull/995
[pr-996]: https://github.com/regclient/regclient/pull/996
[pr-997]: https://github.com/regclient/regclient/pull/997
[pr-998]: https://github.com/regclient/regclient/pull/998
[pr-1005]: https://github.com/regclient/regclient/pull/1005
[pr-1007]: https://github.com/regclient/regclient/pull/1007
[pr-1008]: https://github.com/regclient/regclient/pull/1008
