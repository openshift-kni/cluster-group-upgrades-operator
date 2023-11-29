# Developing in cluster-group-upgrades-operator

## Path structure
The repository's root directory must be in `github.com/openshift-kni/`.
Running `./hack/fix-path-structure.sh` from the repository's root directory will fix the path.
Another option is to clone the repository in the correct path:

```
mkdir -p github.com/openshift-kni/
cd github.com/openshift-kni/
git clone git@github.com:openshift-kni/cluster-group-upgrades-operator.git
```
## Makefile targets

To see all `make` targets, run `make help`

## Linter tests

As of this writing, four linters are run by the `make ci-job` command:

* golint
* golangci-lint
* shellcheck
* bashate

These tests will be run automatically as part of the ci-job test post a pull request an update. Failures will mean your pull request
cannot be merged. It is recommended that you run `make ci-job` regularly as part of development.

## GO formatting

GO has automated formatting. To update code and ensure it is formatted properly, run:<br>`make fmt`

## Updates to bindata

When updating files in a `bindata` directory (eg. `recovery/bindata/update-recovery.sh`), you will need to regenerate the
corresponding go code by running:<br>
`make update-bindata`

## Building image

There are three images built in this repo:
* cluster-group-upgrades-operator
* cluster-group-upgrades-operator-precache
* cluster-group-upgrades-operator-recovery

There are make variables you can set when building the images to customize how they are built and tagged. For example, you can set
ENGINE=podman if your build system uses podman instead of docker. To use a custom repository, you can use the IMAGE_TAG_BASE variable.

To build and push the cluster-group-upgrades-operator image:
`make docker-build ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${MY_REPO_ID}/cluster-group-upgrades-operator`
`make docker-push ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${MY_REPO_ID}/cluster-group-upgrades-operator`

To build and push the cluster-group-upgrades-operator-precache image:
`make docker-build-precache ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${MY_REPO_ID}/cluster-group-upgrades-operator`
`make docker-push-precache ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${MY_REPO_ID}/cluster-group-upgrades-operator`

To build and push the cluster-group-upgrades-operator-recovery image:
`make docker-build-recovery ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${MY_REPO_ID}/cluster-group-upgrades-operator`
`make docker-push-recovery ENGINE=podman VERSION=latest IMAGE_TAG_BASE=quay.io/${MY_REPO_ID}/cluster-group-upgrades-operator`
