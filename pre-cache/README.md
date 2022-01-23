# OCP upgrade image pre-cache #
This directory contains scripts running on spokes where image pre-caching is required.

To build the pre-caching container image and push it to a registry, run:
```bash
VERSION=<Desired version> PRECACHE_IMG=<registry>/<repository>/<image> make docker-build-precache
VERSION=<Desired version> PRECACHE_IMG=<registry>/<repository>/<image> make docker-push-precache
```
For example:
```bash
VERSION=latest PRECACHE_IMG=quay.io/test_images/pre-cache make docker-build-precache
```

