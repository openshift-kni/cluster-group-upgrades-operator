# Developing in cluster-group-upgrades-operator/recovery

## Makefile targets

To see all `make` targets, run `make help`

## Updates to bindata

When updating files in the `bindata` directory, you will need to regenerate the corresponding go code by running:<br>
`make update-bindata`

## Linter tests

As of this writing, four linters are used by the `make check` command, as well as by the configured github workflow
actions:

* lint
* golangci-lint
* shellcheck

It is suggested that you run these tests regularly as part of development.

### golangci-lint

Install golangci-lint in your GO environment by running:<br>
`go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.44.0`

For more information, see:<br><https://golangci-lint.run/usage/install/>

### shellcheck

ShellCheck provides warnings and suggestions for bash scripts. For information on installing, see:<br>
<https://github.com/koalaman/shellcheck#installing>

Example:<br>`dnf install ShellCheck`

## GO formatting

GO has automated formatting. To update code and ensure it is formatted properly, run:<br>`make update-gofmt`

## Building image

To build the image, run `make docker-build-recovery`. If you use a container engine other than docker, such as podman,
specify the `ENGINE` variable. To tag the image with your own registry, specify the `IMAGE_TAG_BASE` tag. For
example:<br>
`make docker-build-recovery ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${USER}/cluster-group-upgrades-operator`

To push the image, run `make docker-push-recovery`. For example:
`make docker-push-recovery ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${USER}/cluster-group-upgrades-operator`
