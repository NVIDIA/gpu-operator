# Release v0.11.5

Security:

- Prevent https to non-https downgrades and localhost redirects. ([PR 1093][pr-1093])
- Forbid sending auth on redirects. ([PR 1095][pr-1095])

Features:

- Add regbot `manifest.descriptor` to the sandbox. ([PR 1091][pr-1091])

Contributors:

- @GimmyDatBeeR
- @sudo-bmitch

[pr-1091]: https://github.com/regclient/regclient/pull/1091
[pr-1093]: https://github.com/regclient/regclient/pull/1093
[pr-1095]: https://github.com/regclient/regclient/pull/1095
