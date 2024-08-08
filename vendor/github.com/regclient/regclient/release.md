# Release v0.7.1

[PR 798][pr-798] fixes an issue where a malicious registry could return a pinned manifest different from the request.
Commands like `regctl manifest get $image@$digest` will now verify the digest of the returned manifest matches the request rather than the registry headers.

Security updates:

- Validate the digest of the ref when provided. ([PR 798][pr-798])

Features:

- Add a `WithDockerCredsFile() regclient.Opt`. ([PR 784][pr-784])
- Add `regctl artifact get --config` option to only return the config. ([PR 795][pr-795])

Fixes:

- Detect `amd64` variants for `--platform local`. ([PR 782][pr-782])
- Mod tracking of changed manifests. ([PR 783][pr-783])
- Tar path separator should always be a `/`. ([PR 788][pr-788])

Other Changes:

- Remove docker build cache in GHA. ([PR 780][pr-780])

Contributors:

- @mmonaco
- @stormyyd
- @sudo-bmitch

[pr-780]: https://github.com/regclient/regclient/pull/780
[pr-782]: https://github.com/regclient/regclient/pull/782
[pr-783]: https://github.com/regclient/regclient/pull/783
[pr-784]: https://github.com/regclient/regclient/pull/784
[pr-788]: https://github.com/regclient/regclient/pull/788
[pr-795]: https://github.com/regclient/regclient/pull/795
[pr-798]: https://github.com/regclient/regclient/pull/798
