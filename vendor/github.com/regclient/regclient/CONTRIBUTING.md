# Contributing

## Reporting security issues

Please see [SECURITY.md](security.md) for the process to report security issues.

## Reporting other issues

Please search for similar issues and if none are seen, report an issue at [github.com/regclient/regclient/issues](https://github.com/regclient/regclient/issues)

## Code style

This project attempts to follow these principles:

- Code is canonical Go, following styles and patterns commonly used by the Go community.
- Dependencies outside of the Go standard library should be minimized.
- Dependencies should be pinned to a specific digest and tracked by Go or version-check.
- Unit tests are strongly encouraged with a focus on test coverage of the successful path and common errors.
- Linters and other style formatting tools are used, please run `make all` before committing any changes.

## Pull requests

PRs are welcome following the below guides:

- For anything beyond a minor fix, opening an issue is suggested to discuss possible solutions.
- Changes should be rebased on the main branch.
- Changes should be squashed to a single commit per logical change.

All changes must be signed (`git commit -s`) to indicate you agree to the [Developer Certificate or Origin](https://developercertificate.org/):

```text
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

The sign-off will include the following message in your commit:

```text
Signed-off-by: Your Name <your-email@example.org>
```

This needs to be your real name, no aliases please.
