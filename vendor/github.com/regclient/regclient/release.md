# Release v0.7.2

Breaking Changes:

The breaking changes are to internal methods and undocumented features that should not be encountered by users.

- Update scheme to use pqueue instead of throttle. ([PR 803][pr-803])
- Removes an undocumented API for deleting images from Hub. ([PR 803][pr-803])
- `config.Host.Throttle()` has been removed. Use `scheme.Throttler` instead. ([PR 813][pr-813])

Features:

- Significant refactor of http APIs to speed up image copies. ([PR 803][pr-803])
- Add a priority queue for network requests. ([PR 803][pr-803])
- Move logging into transport and rework backoff. ([PR 803][pr-803])
- Remove default rate limit. ([PR 803][pr-803])
- Add priority queue algorithm and reorder image copy steps. ([PR 803][pr-803])
- Consolidate warnings. ([PR 810][pr-810])
- Limit number of retries for a request. ([PR 812][pr-812])
- Add default host config. ([PR 821][pr-821])

Fixes:

- Update GHA output generating steps. ([PR 800][pr-800])
- Lookup referrers when registry does not give digest with head. ([PR 801][pr-801])
- Support auth on redirect. ([PR 805][pr-805])
- Prevent data race when reading blob and seeking. ([PR 814][pr-814])
- Detect integer overflows on type conversion. ([PR 830][pr-830])
- Add a warning if syft is not installed. ([PR 841][pr-841])
- Race condition in the pqueue tests. ([PR 843][pr-843])
- Dedup warnings on image mod. ([PR 846][pr-846])

Chores:

- Update staticcheck and fix linter warnings for Go 1.23. ([PR 804][pr-804])
- Remove digest calculation from reghttp. ([PR 803][pr-803])
- Remove `ReqPerSec` in tests. ([PR 806][pr-806])
- Move throttle from `config` to `reghttp`. ([PR 813][pr-813])
- Refactoring to remove globals in regsync. ([PR 815][pr-815])
- Refactor to remove globals in regbot. ([PR 816][pr-816])
- Remove throttle package. ([PR 817][pr-817])
- Update version-bump config for processors. ([PR 828][pr-828])
- Update config to use yaml anchors and aliases ([PR 829][pr-829])
- Do not automatically assign myself to GitHub issues. ([PR 831][pr-831])
- Remove OpenSSF scorecard and best practices. ([PR 832][pr-832])
- Update docker image base filesystem. ([PR 837][pr-837])

Contributors:

- @sudo-bmitch

[pr-800]: https://github.com/regclient/regclient/pull/800
[pr-801]: https://github.com/regclient/regclient/pull/801
[pr-804]: https://github.com/regclient/regclient/pull/804
[pr-803]: https://github.com/regclient/regclient/pull/803
[pr-805]: https://github.com/regclient/regclient/pull/805
[pr-806]: https://github.com/regclient/regclient/pull/806
[pr-810]: https://github.com/regclient/regclient/pull/810
[pr-812]: https://github.com/regclient/regclient/pull/812
[pr-813]: https://github.com/regclient/regclient/pull/813
[pr-814]: https://github.com/regclient/regclient/pull/814
[pr-815]: https://github.com/regclient/regclient/pull/815
[pr-816]: https://github.com/regclient/regclient/pull/816
[pr-817]: https://github.com/regclient/regclient/pull/817
[pr-821]: https://github.com/regclient/regclient/pull/821
[pr-828]: https://github.com/regclient/regclient/pull/828
[pr-829]: https://github.com/regclient/regclient/pull/829
[pr-830]: https://github.com/regclient/regclient/pull/830
[pr-831]: https://github.com/regclient/regclient/pull/831
[pr-832]: https://github.com/regclient/regclient/pull/832
[pr-837]: https://github.com/regclient/regclient/pull/837
[pr-841]: https://github.com/regclient/regclient/pull/841
[pr-843]: https://github.com/regclient/regclient/pull/843
[pr-846]: https://github.com/regclient/regclient/pull/846
