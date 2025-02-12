# Release v0.8.1

Security:

- Go v1.23.6 fixes CVE-2025-22866. ([PR 906][pr-906])

Features:

- Improve regctl arg completion. ([PR 895][pr-895])
- Add cobra command for documentation. ([PR 900][pr-900])

Fixes:

- Do not request offline refresh token. ([PR 893][pr-893])
- Ignore unsupported entries in docker config. ([PR 894][pr-894])
- Align log levels with slog. ([PR 901][pr-901])
- Interval overrides a default schedule in regsync and regbot. ([PR 904][pr-904])

Miscellaneous:

- Adding a logo. ([PR 889][pr-889])

Contributors:

- @obaibula
- @sudo-bmitch

[pr-889]: https://github.com/regclient/regclient/pull/889
[pr-893]: https://github.com/regclient/regclient/pull/893
[pr-894]: https://github.com/regclient/regclient/pull/894
[pr-895]: https://github.com/regclient/regclient/pull/895
[pr-900]: https://github.com/regclient/regclient/pull/900
[pr-901]: https://github.com/regclient/regclient/pull/901
[pr-904]: https://github.com/regclient/regclient/pull/904
[pr-906]: https://github.com/regclient/regclient/pull/906
