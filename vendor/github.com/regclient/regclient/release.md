# Release v0.4.7

This is a repeat of v0.4.6 with fixes to GitHub Actions performing the release.

## Release v0.4.6

Breaking Changes:

- Tag details are no longer available in `version` commands ([PR 305][pr-305])
- `regclient.VCSRef` and `VCSTag` variables removed ([PR 305][pr-305])
- Detailed version support removed for Go 1.17 and earlier ([PR 305][pr-305])
- Artifact manifest is experimental ([PR 372][pr-372])

New Features:

- Make image builds reproducible ([PR 309][pr-309])
- Improve handling of network failures on blob push ([PR 324][pr-324])
- Add configurable limits on concurrency and requests per second ([PR 330][pr-330])
- regbot: adding options to `repo.ls` to support paginated responses ([PR 332][pr-332])
- Improve retries of blob copies between registries ([PR 333][pr-333])
- `regctl registry login` supports reading the password from stdin ([PR 348][pr-348])
- Add `mod.WithManifestToOCIReferrers()` to convert Docker to OCI referrers ([PR 349][pr-349])
- Add `regctl image mod --to-oci-referrers` ([PR 349][pr-349])
- Add SBOMs to artifacts to images ([PR 351][pr-351])
- Add `regctl manifest head` and `regctl blob head` commands ([PR 358][pr-358])
- Adding platform option to `regctl artifact` commands ([PR 359][pr-359])
- Adding platform option to the `ReferrerList` API ([PR 359][pr-359])
- Add `regctl artifact tree` command ([PR 361][pr-361])
- Binary artifacts are signed with cosign keyless signing ([PR 365][pr-365])
- Add referrers support to regsync ([PR 366][pr-366])

Bug Fixes:

- Fix error handling on manifests created by `ManifestHead` ([PR 343][pr-343])
- Fix `regctl image mod` for non-tar layers and non-image configs ([PR 347][pr-347])
- Converting to OCI now handles OCI manifests with mixed media types inside manifest ([PR 353][pr-353])
- Fix issue importing images created with tar ([PR 355][pr-355])
- Fallback to `GET` when `HEAD` request is missing digest header ([PR 363][pr-363])
- Fix handling of data field ([PR 368][pr-368])

Other Changes:

- Add help text to Makefile ([PR 308][pr-308])
- Make `artifactType` field optional for artifact manifests ([PR 327][pr-327])
- Fix reproducible builds with docker provenance ([PR 350][pr-350])
- Update Go support to 1.20 ([PR 356][pr-356])
- Upgrade Go to 1.20.1 ([PR 362][pr-362])
- Pin the version of staticcheck ([PR 364][pr-364])

[pr-305]: https://github.com/regclient/regclient/pull/305
[pr-308]: https://github.com/regclient/regclient/pull/308
[pr-309]: https://github.com/regclient/regclient/pull/309
[pr-324]: https://github.com/regclient/regclient/pull/324
[pr-327]: https://github.com/regclient/regclient/pull/327
[pr-330]: https://github.com/regclient/regclient/pull/330
[pr-332]: https://github.com/regclient/regclient/pull/332
[pr-333]: https://github.com/regclient/regclient/pull/333
[pr-343]: https://github.com/regclient/regclient/pull/343
[pr-347]: https://github.com/regclient/regclient/pull/347
[pr-348]: https://github.com/regclient/regclient/pull/348
[pr-349]: https://github.com/regclient/regclient/pull/349
[pr-350]: https://github.com/regclient/regclient/pull/350
[pr-351]: https://github.com/regclient/regclient/pull/351
[pr-353]: https://github.com/regclient/regclient/pull/353
[pr-355]: https://github.com/regclient/regclient/pull/355
[pr-356]: https://github.com/regclient/regclient/pull/356
[pr-358]: https://github.com/regclient/regclient/pull/358
[pr-359]: https://github.com/regclient/regclient/pull/359
[pr-361]: https://github.com/regclient/regclient/pull/361
[pr-362]: https://github.com/regclient/regclient/pull/362
[pr-363]: https://github.com/regclient/regclient/pull/363
[pr-364]: https://github.com/regclient/regclient/pull/364
[pr-365]: https://github.com/regclient/regclient/pull/365
[pr-366]: https://github.com/regclient/regclient/pull/366
[pr-368]: https://github.com/regclient/regclient/pull/368
[pr-372]: https://github.com/regclient/regclient/pull/372
