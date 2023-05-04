# regclient

![GitHub Workflow Status](https://img.shields.io/github/workflow/status/regclient/regclient/Go?label=Go%20build)
![GitHub Workflow Status](https://img.shields.io/github/workflow/status/regclient/regclient/Docker?label=Docker%20build)
![GitHub](https://img.shields.io/github/license/regclient/regclient)
[![Go Reference](https://pkg.go.dev/badge/github.com/regclient/regclient.svg)](https://pkg.go.dev/github.com/regclient/regclient)

Client interface for the registry API.
This includes `regctl` for a command line interface to manage registries.

![regctl demo](docs/demo.gif)

## regclient Features

- Provides a client interface to interacting with registries.
- Images may be inspected without pulling the layers, allowing quick access to the image manifest and configuration.
- Tags may be listed for a repository.
- Repositories may be listed from a registry (if supported).
- Copying an image only pulls layers when needed, allowing images to be quickly retagged or promoted across repositories.
- Multi-platform images are supported, allowing all platforms to be copied between registries.
- Digest tags used by projects like sigstore/cosign are supported, allowing signature, attestation, and SBOM metadata to be copied with the image.
- Digests may be queried for a tag without pulling the manifest.
- Rate limits may be queried from the registry without pulling an image (useful for Docker Hub).
- Images may be imported and exported to both OCI and Docker formatted tar files.
- OCI Layout is supported for copying images to and from a local directory.
- Delete APIs have been provided for tags, manifests, and blobs (the tag deletion will only delete a single tag even if multiple tags point to the same digest).
- Registry logins are imported from docker when available
- Self signed, insecure, and http-only registries are all supported.
- Requests will retry and fall back to chunked uploads when network issues are encountered.

## regctl Features

`regctl` is a CLI interface to the `regclient` library.
In addition to the features listed for `regclient`, `regctl` adds the following abilities:

- Formatting output with templates.
- Push and pull arbitrary artifacts.

## regsync features

`regsync` is an image mirroring tool.
It will copy images between two locations with the following additional features:

- Uses a yaml configuration.
- The `regclient` copy is used to only pull needed layers, supporting multi-platform, and additional metadata.
- Can use user's docker configuration for registry credentials.
- Ability to run on a cron schedule, one time synchronization, or only check for stale images.
- Ability to backup previous target image before overwriting.
- Ability to postpone mirror step when rate limit is below a threshold.
- Ability to mirror multiple images concurrently.

## regbot features

`regbot` is a scripting tool on top of the `regclient` API with the following features:

- Runs user provided scripts based on a yaml configuration.
- Scripts are written in Lua and executed directly in Go.
- Can run on a cron schedule or a one time execution.
- Dry-run option can be used for testing.
- Built-in functions include:
  - Repository list
  - Tag list
  - Image manifest (either head or get, and optional resolving multi-platform reference)
  - Image config (this includes the creation time, labels, and other details shown in a `docker image inspect`)
  - Image ratelimit and a wait function to delay the script when ratelimit remaining is below a threshold
  - Image copy
  - Manifest delete
  - Tag delete

## Development Status

This project is in active development.
Various Go APIs may change, but efforts will be made to provide aliases and stubs for any removed API.

## Building

```shell
git clone https://github.com/regclient/regclient.git
cd regclient
make
```

## Downloading Binaries

Binaries are available on the [releases
page](https://github.com/regclient/regclient/releases).

The latest release can be downloaded using curl (adjust "regctl" and
"linux-amd64" for the desired command and your own platform):

```shell
curl -L https://github.com/regclient/regclient/releases/latest/download/regctl-linux-amd64 >regctl
chmod 755 regctl
```

Merges into the main branch also have binaries available as artifacts within [GitHub Actions](https://github.com/regclient/regclient/actions/workflows/go.yml?query=branch%3Amain)

## Running as a Container

You can run `regctl`, `regsync`, and `regbot` in a container.

For `regctl` (include a `-t` for any commands that require a tty, e.g. `registry login`):

```shell
docker container run -i --rm --net host \
  -v regctl-conf:/home/appuser/.regctl/ \
  regclient/regctl:latest --help
```

For `regsync`:

```shell
docker container run -i --rm --net host \
  -v "$(pwd)/regsync.yml:/home/appuser/regsync.yml" \
  regclient/regsync:latest -c /home/appuser/regsync.yml check
```

For `regbot`:

```shell
docker container run -i --rm --net host \
  -v "$(pwd)/regbot.yml:/home/appuser/regbot.yml" \
  regclient/regbot:latest -c /home/appuser/regbot.yml once --dry-run
```

Or on Linux and Mac environments, you can run `regctl` as your own user and save
configuration settings, use docker credentials, and use any docker certs:

```shell
docker container run -i --rm --net host \
  -u "$(id -u):$(id -g)" -e HOME -v $HOME:$HOME \
  -v /etc/docker/certs.d:/etc/docker/certs.d:ro \
  regclient/regctl:latest --help
```

And `regctl` can be packaged as a shell script with:

```shell
cat >regctl <<EOF
#!/bin/sh
opts=""
case "\$*" in
  "registry login"*) opts="-t";;
esac
docker container run \$opts -i --rm --net host \\
  -u "\$(id -u):\$(id -g)" -e HOME -v \$HOME:\$HOME \\
  -v /etc/docker/certs.d:/etc/docker/certs.d:ro \\
  regclient/regctl:latest "\$@"
EOF
chmod 755 regctl
./regctl --help
```

Images are also included with an alpine base, which are useful for CI pipelines that expect the container to include a `/bin/sh`.
These alpine based images also include the `ecr-login` and `gcr` credential helpers.

## Installing as a Docker CLI Plugin

To install `regctl` as a docker CLI plugin:

```shell
make plugin-user # install for the current user
make plugin-host # install for all users on the host (requires sudo)
```

Once installed as a plugin, you can access it from the docker CLI:

```shell
$ docker regctl --help
Utility for accessing docker registries
More details at https://github.com/regclient/regclient

Usage:
  regctl <cmd> [flags]
  regctl [command]

Available Commands:
  help        Help about any command
  image       manage images
  layer       manage image layers/blobs
  registry    manage registries
  tag         manage tags
...
```

## Usage

See the [project documentation](docs/README.md).
