# Release v0.8.3

Features:

- Add `ref.AddDigest` method that does not unset the tag. ([PR 910][pr-910])
- Adding a `regctl registry whoami` command. ([PR 912][pr-912])
- Improve `regctl image check-base` output. ([PR 917][pr-917])
- regsync option to abort on errors. ([PR 924][pr-924])
- Improve fallback tag handling. ([PR 925][pr-925])
- regctl flag to ignore missing images on delete. ([PR 930][pr-930])

Fixes:

- Validate registry names. ([PR 911][pr-911])
- Escape regexp example. ([PR 920][pr-920])
- Auth header parsing. ([PR 936][pr-936])

Changes:

- Update supported Go releases to 1.22, 1.23, and 1.24. ([PR 909][pr-909])
- Modernize Go to the 1.22 specs. ([PR 910][pr-910])
- Refactor cobra commands. ([PR 915][pr-915])
- Include Docker Hub repository documentation. ([PR 918][pr-918])
- Move documentation pointers to the website. ([PR 939][pr-939])

Contributors:

- @sudo-bmitch

[pr-909]: https://github.com/regclient/regclient/pull/909
[pr-910]: https://github.com/regclient/regclient/pull/910
[pr-911]: https://github.com/regclient/regclient/pull/911
[pr-912]: https://github.com/regclient/regclient/pull/912
[pr-915]: https://github.com/regclient/regclient/pull/915
[pr-917]: https://github.com/regclient/regclient/pull/917
[pr-918]: https://github.com/regclient/regclient/pull/918
[pr-920]: https://github.com/regclient/regclient/pull/920
[pr-924]: https://github.com/regclient/regclient/pull/924
[pr-925]: https://github.com/regclient/regclient/pull/925
[pr-930]: https://github.com/regclient/regclient/pull/930
[pr-936]: https://github.com/regclient/regclient/pull/936
[pr-939]: https://github.com/regclient/regclient/pull/939
