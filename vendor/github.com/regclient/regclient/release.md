# Release v0.7.0

CVEs:

- CVE-2024-24790 fix included with Go 1.22.4 upgrade. ([PR 762][pr-762])
- CVE-2024-24791 fix included with Go 1.22.5 upgrade. ([PR 777][pr-777])

Breaking:

- `regctl registry set` and `regctl registry login` will return a non-zero if the ping fails. ([PR 751][pr-751])
- Removed `WithFS` which required access to an internal interface to use. ([PR 772][pr-772])

Features:

- Add an experimental `regctl ref` command. ([PR 765][pr-765])
- Support digest algorithms beyond sha256. ([PR 776][pr-776])
- Support modifying the digest algorithm on an image. ([PR 776][pr-776])
- Experimental support for pushing tagged manifests with different digest algorithms. ([PR 778][pr-778])

Fixes:

- Prevent panic on interrupted image mod. ([PR 746][pr-746])
- Enable deletion on olareg tests. ([PR 758][pr-758])
- Allow `~` (tilde) in ocidir reference paths. ([PR 763][pr-763])
- Allow well known architectures as a platform. ([PR 771][pr-771])
- Validate digests before calling methods that could panic. ([PR 776][pr-776])

Other changes:

- Refactor pulling manifests by platform. ([PR 768][pr-768])
- Cleanup Dockerfile linter warnings. ([PR 770][pr-770])
- Enable docker caching of GHA builds. ([PR 773][pr-773])
- Include a contributor list in the readme. ([PR 774][pr-774])

Contributors:

- @sudo-bmitch
- @thesayyn

[pr-746]: https://github.com/regclient/regclient/pull/746
[pr-751]: https://github.com/regclient/regclient/pull/751
[pr-758]: https://github.com/regclient/regclient/pull/758
[pr-762]: https://github.com/regclient/regclient/pull/762
[pr-763]: https://github.com/regclient/regclient/pull/763
[pr-765]: https://github.com/regclient/regclient/pull/765
[pr-768]: https://github.com/regclient/regclient/pull/768
[pr-770]: https://github.com/regclient/regclient/pull/770
[pr-771]: https://github.com/regclient/regclient/pull/771
[pr-772]: https://github.com/regclient/regclient/pull/772
[pr-773]: https://github.com/regclient/regclient/pull/773
[pr-774]: https://github.com/regclient/regclient/pull/774
[pr-776]: https://github.com/regclient/regclient/pull/776
[pr-777]: https://github.com/regclient/regclient/pull/777
[pr-778]: https://github.com/regclient/regclient/pull/778
