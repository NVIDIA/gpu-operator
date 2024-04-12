# Release v0.6.0

Breaking:

- `regctl artifact put` no longer includes the filename annotation by default. Use `--file-title` to include. ([PR 659][pr-659])
- Dropping Go 1.19 support ([PR 656][pr-656])
- The platform string for windows images no longer includes the non-standard OS Version value. ([PR 685][pr-685])

Fixes:

- Allow pushing artifacts without an artifactType value. ([PR 658][pr-658])
- Image mod where created image is in a different repository ([PR 662][pr-662])
- Improve returned errors from `regclient.ImageCopy`. ([PR 663][pr-663])
- Cancel blob uploads on failures. ([PR 666][pr-666])
- Allow ctrl-c on `regctl registry login` ([PR 671][pr-671])
- Promoting annotations should ignore child manifests that have been removed from the tree. ([PR 675][pr-675])
- Pin base image digest in build scripts to match Dockerfile pins. ([PR 678][pr-678])
- Error wrapping fixed in several locations. ([PR 682][pr-682])
- Platform selection now finds the best match rather than the first compatible match. ([PR 685][pr-685])
- Update registry versions in CI tests. ([PR 687][pr-687])
- Missing lines from diff context. ([PR 688][pr-688])
- Replace `syft packages` with `syft scan`. ([PR 695][pr-695])
- Image mod can manage the data file on the config descriptor of artifacts. ([PR 697][pr-697])

Features:

- Adding Go 1.22 support ([PR 656][pr-656])
- Add `BlobDelete` support for ocidir references. ([PR 669][pr-669])
- Add `regctl blob delete` command. ([PR 669][pr-669])
- Support formatting output on `regctl registry config`. ([PR 673][pr-673])
- Add image mod ability to promote common annotations in the child images to the index. ([PR 674][pr-674])
- Specifying windows OS Version now uses a comma separated syntax in the platform string. ([PR 685][pr-685])
- Detect AMD64 variant when looking up local platform. ([PR 692][pr-692])
- Add ability to set the config platform setting with `regctl image mod`. ([PR 693][pr-693])
- Image mod support for setting the entrypoint and cmd. ([PR 694][pr-694])

Deprecations:

- Errors in `types` are moved to the `errs` package. ([PR 686][pr-686])
- MediaTypes in `types` are moved to the `mediatype` package. ([PR 686][pr-686])
- Descriptor and associated variables in `types` are moved to the `descriptor` package. ([PR 686][pr-686])
- `github.com/regclient/regclient/regclient` (3 levels of regclient) deprecations are now identified by the standard comment to trigger linters. ([PR 686][pr-686])

Other changes:

- Update OSV scanner to monitor for unapproved licenses. ([PR 655][pr-655])
- Include an API example in the Go docs. ([PR 657][pr-657])
- Add examples to regctl help messages. ([PR 660][pr-660])
- Include the Go Report Card badge. ([PR 664][pr-664])
- Document the availability of the GitHub Actions installer for `regctl`. ([PR 665][pr-665])
- Add examples to regctl help messages. ([PR 672][pr-672])
- Redesign how annotations are added to the regclient images. ([PR 676][pr-676])
- Remove uuid dependency from test code, replace with a random string generator. ([PR 677][pr-677])
- Manage base image annotation with version-bump. ([PR 679][pr-679])
- Use `t.Fatal` where appropriate. ([PR 680][pr-680])
- Remove wraperr package. ([PR 681][pr-681])
- Add links to the GHA workflow badges. ([PR 683][pr-683])
- Include a download count badge. ([PR 684][pr-684])
- Refactoring `types` package to avoid circular dependency issues. ([PR 686][pr-686])
- Cleanup unused parameters on private functions. ([PR 698][pr-698])
- Resume push of SBOMs to Docker Hub. ([PR 701][pr-701])

Contributors:

- @sudo-bmitch

[pr-655]: https://github.com/regclient/regclient/pull/655
[pr-656]: https://github.com/regclient/regclient/pull/656
[pr-657]: https://github.com/regclient/regclient/pull/657
[pr-658]: https://github.com/regclient/regclient/pull/658
[pr-659]: https://github.com/regclient/regclient/pull/659
[pr-660]: https://github.com/regclient/regclient/pull/660
[pr-662]: https://github.com/regclient/regclient/pull/662
[pr-663]: https://github.com/regclient/regclient/pull/663
[pr-664]: https://github.com/regclient/regclient/pull/664
[pr-665]: https://github.com/regclient/regclient/pull/665
[pr-666]: https://github.com/regclient/regclient/pull/666
[pr-669]: https://github.com/regclient/regclient/pull/669
[pr-671]: https://github.com/regclient/regclient/pull/671
[pr-672]: https://github.com/regclient/regclient/pull/672
[pr-673]: https://github.com/regclient/regclient/pull/673
[pr-674]: https://github.com/regclient/regclient/pull/674
[pr-675]: https://github.com/regclient/regclient/pull/675
[pr-676]: https://github.com/regclient/regclient/pull/676
[pr-677]: https://github.com/regclient/regclient/pull/677
[pr-678]: https://github.com/regclient/regclient/pull/678
[pr-679]: https://github.com/regclient/regclient/pull/679
[pr-680]: https://github.com/regclient/regclient/pull/680
[pr-681]: https://github.com/regclient/regclient/pull/681
[pr-682]: https://github.com/regclient/regclient/pull/682
[pr-683]: https://github.com/regclient/regclient/pull/683
[pr-684]: https://github.com/regclient/regclient/pull/684
[pr-686]: https://github.com/regclient/regclient/pull/686
[pr-685]: https://github.com/regclient/regclient/pull/685
[pr-687]: https://github.com/regclient/regclient/pull/687
[pr-688]: https://github.com/regclient/regclient/pull/688
[pr-692]: https://github.com/regclient/regclient/pull/692
[pr-693]: https://github.com/regclient/regclient/pull/693
[pr-694]: https://github.com/regclient/regclient/pull/694
[pr-695]: https://github.com/regclient/regclient/pull/695
[pr-697]: https://github.com/regclient/regclient/pull/697
[pr-698]: https://github.com/regclient/regclient/pull/698
[pr-701]: https://github.com/regclient/regclient/pull/701
