#!/bin/bash

set -e

rm -rf /host/tmp/precache
cp -a /opt/precache /host/tmp/
cp -rf /etc/config /host/tmp/precache/config
chroot /host /tmp/precache/check_space
[ $? -ne 0 ] && echo "not enough space for precaching" && exit 1
chroot /host /tmp/precache/release
chroot /host /tmp/precache/olm
chroot /host /tmp/precache/pull
