#!/usr/bin/bash

set -e

# Setup the environment for running podman.
mkdir /host/dev
mknod -m 0666 /host/dev/null c 1 3
mkdir /host/dev/shm
/host/usr/bin/mount -t tmpfs -o rw,nosuid,nodev,noexec,relatime tmpfs /host/dev/shm

ln -s usr/bin /host/bin

# Setup the environment for precaching.
rm -rf /host/tmp/precache
rm -f /host/tmp/images.txt
cp -a /opt/precache /host/tmp/
cp -rf /etc/config /host/tmp/precache/config

# Check the available space for the OCP upgrade case.
if [ -n "$(cat /etc/config/platform.image)" ]; then
    /opt/precache/check_space
fi
chroot /host /tmp/precache/release
chroot /host /tmp/precache/olm
chroot /host /tmp/precache/pull
