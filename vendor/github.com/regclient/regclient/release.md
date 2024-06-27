# Release v0.6.1

CVEs:

- Go update fixes CVE-2024-24788. ([PR 739][pr-739])

Breaking:

- pkg/archive.Compress no longer decompresses the input. ([PR 732][pr-732])

Features:

- Add the `regclient.ImageConfig` method. ([PR 706][pr-706])
- Add ability to modify the layer compression. ([PR 730][pr-730])
- Add support for zstd compressed layers. ([PR 732][pr-732])
- Add image mod ability to append layers to an image. ([PR 736][pr-736])
- `regctl image mod` add layer from directory. ([PR 740][pr-740])

Fixes:

- Override the Go version used by the OSV Scanner. ([PR 691][pr-691])
- Validate media types on `regctl artifact put`. ([PR 707][pr-707])
- Use the provided descriptor in the BlobGet/Head to a registry. ([PR 724][pr-724])
- Replace "whitelist" with "known list" for inclusivity. ([PR 725][pr-725])
- Handle nil pointer when config file is a directory. ([PR 738][pr-738])

Chores:

- Limit token permission on the coverage action. ([PR 705][pr-705])
- Clarify `regctl manifest head --platform` will trigger a get request. ([PR 713][pr-713])
- Reenable OSV Scanner weekly check in GitHub Actions. ([PR 715][pr-715])
- Add fuzzing tests for compression. ([PR 741][pr-741])

Contributors:

- @sudo-bmitch

[pr-691]: https://github.com/regclient/regclient/pull/691
[pr-705]: https://github.com/regclient/regclient/pull/705
[pr-706]: https://github.com/regclient/regclient/pull/706
[pr-707]: https://github.com/regclient/regclient/pull/707
[pr-713]: https://github.com/regclient/regclient/pull/713
[pr-715]: https://github.com/regclient/regclient/pull/715
[pr-724]: https://github.com/regclient/regclient/pull/724
[pr-725]: https://github.com/regclient/regclient/pull/725
[pr-730]: https://github.com/regclient/regclient/pull/730
[pr-732]: https://github.com/regclient/regclient/pull/732
[pr-736]: https://github.com/regclient/regclient/pull/736
[pr-738]: https://github.com/regclient/regclient/pull/738
[pr-739]: https://github.com/regclient/regclient/pull/739
[pr-740]: https://github.com/regclient/regclient/pull/740
[pr-741]: https://github.com/regclient/regclient/pull/741
