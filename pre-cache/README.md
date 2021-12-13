# OCP upgrade image pre-cache #
This directory contains scripts running on spokes where image pre-caching is required.

To build the pre-caching container image, run:
```bash
PRECACHE_IMG=<Your image>:<version> make docker-build-precache
```

