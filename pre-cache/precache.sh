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

# Check the available space for the OCP upgrade case or for pre-caching additional images
{ [ -n "$(cat /etc/config/platform.image)" ] || [ -n "$(cat /etc/config/additionalImages)" ]; } && check_disk_space=1 || check_disk_space=0
if [[ $check_disk_space == 1 ]]; then
    /opt/precache/check_space $(cat /etc/config/spaceRequired)
fi

chroot /host /tmp/precache/release
chroot /host /tmp/precache/olm
chroot /host /tmp/precache/pull

# Check disk space usage post pre-caching to alert if kubelet Garbage Collection will be triggered
if [[ $check_disk_space == 1 ]]; then
    /opt/precache/check_space
fi
