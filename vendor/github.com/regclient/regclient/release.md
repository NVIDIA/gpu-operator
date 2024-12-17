# Release v0.8.0

## Highlights

There are three headline changes in this release: slog support, external referrers, and deprecating legacy packages.

This release switches from logrus to slog.
Migration methods are included to minimize the impact on existing users.
Anyone parsing the logging output from regctl, regsync, and regbot will notice the format has changed.

External referrers allow referrers to be pushed and pulled from a separate repository from the subject image.
This feature requires users to provide the external repository themselves since a registry has no way to communicate this to the user.
An example use case of this feature are third parties, like security scanners, providing attestations of images they do not control.

Legacy packages have been disabled by default and will eventually be removed.
To continue using legacy packages until their removal, you may compile with `-tags legacy`.

## Breaking

- Breaking: Warning handlers switched from `logrus` to `slog` which will only impact those with a custom warning handler. ([PR 847][pr-847])
- Breaking: Disable legacy packages by default. ([PR 852][pr-852])

## Features

- Feat: Refactor logging to use log/slog. ([PR 847][pr-847])
- Feat: Switch regbot to slog. ([PR 849][pr-849])
- Feat: Switch regctl to slog. ([PR 850][pr-850])
- Feat: Switch regsync to slog. ([PR 851][pr-851])
- Feat: Move logrus calls into files excluded by wasm. ([PR 853][pr-853])
- Feat: Allow plus in ocidir path. ([PR 856][pr-856])
- Feat: Support referrers in an external repository. ([PR 866][pr-866])
- Feat: Image mod environment variables. ([PR 867][pr-867])
- Feat: Include source in referrers response. ([PR 870][pr-870])
- Feat: Add external flag to regctl artifact put. ([PR 873][pr-873])
- Feat: Copy image with external referrers. ([PR 874][pr-874])
- Feat: Document community maintained packages. ([PR 878][pr-878])
- Feat: Support external referrers in regsync. ([PR 881][pr-881])
- Feat: Support incomplete subject descriptor. ([PR 885][pr-885])

## Fixes

- Fix: Inject release notes by file. ([PR 854][pr-854])
- Fix: Platform test for darwin/macos should not add variant. ([PR 879][pr-879])
- Fix: Handle repeated digest in copy with external referrers. ([PR 882][pr-882])

## Chores

- Chore: Improve error message when inspecting artifacts. ([PR 862][pr-862])
- Chore: Remove unused short arg parameters. ([PR 877][pr-877])

## Contributors

- @sudo-bmitch

[pr-847]: https://github.com/regclient/regclient/pull/847
[pr-849]: https://github.com/regclient/regclient/pull/849
[pr-850]: https://github.com/regclient/regclient/pull/850
[pr-851]: https://github.com/regclient/regclient/pull/851
[pr-852]: https://github.com/regclient/regclient/pull/852
[pr-853]: https://github.com/regclient/regclient/pull/853
[pr-854]: https://github.com/regclient/regclient/pull/854
[pr-856]: https://github.com/regclient/regclient/pull/856
[pr-862]: https://github.com/regclient/regclient/pull/862
[pr-866]: https://github.com/regclient/regclient/pull/866
[pr-867]: https://github.com/regclient/regclient/pull/867
[pr-870]: https://github.com/regclient/regclient/pull/870
[pr-873]: https://github.com/regclient/regclient/pull/873
[pr-874]: https://github.com/regclient/regclient/pull/874
[pr-877]: https://github.com/regclient/regclient/pull/877
[pr-878]: https://github.com/regclient/regclient/pull/878
[pr-879]: https://github.com/regclient/regclient/pull/879
[pr-881]: https://github.com/regclient/regclient/pull/881
[pr-882]: https://github.com/regclient/regclient/pull/882
[pr-885]: https://github.com/regclient/regclient/pull/885
