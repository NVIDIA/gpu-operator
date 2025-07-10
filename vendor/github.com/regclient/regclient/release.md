# Release v0.9.0

Breaking:

- Drop support for 3rd Go release because of upstream forced upgrades (see <https://github.com/golang/go/issues/69095>). ([PR 948][pr-948])

Features:

- Add a script to reproduce regclient images. ([PR 940][pr-940])
- Support IPv6 hosts. ([PR 956][pr-956])

Fixes:

- Convert  docker attestations built with `oci-artifact=true`. ([PR 949][pr-949])
- Allow duplicate keys in yaml config. ([PR 952][pr-952])

Miscellaneous:

- Migrate yaml library. ([PR 947][pr-947])
- Convert the build to use OCI style attestations. ([PR 950][pr-950])

Contributors:

- @JimmyMa
- @sudo-bmitch

[pr-940]: https://github.com/regclient/regclient/pull/940
[pr-947]: https://github.com/regclient/regclient/pull/947
[pr-948]: https://github.com/regclient/regclient/pull/948
[pr-949]: https://github.com/regclient/regclient/pull/949
[pr-950]: https://github.com/regclient/regclient/pull/950
[pr-952]: https://github.com/regclient/regclient/pull/952
[pr-956]: https://github.com/regclient/regclient/pull/956
