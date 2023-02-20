#!/bin/bash

set -e

rm -rf /host/tmp/precache
rm -f /host/tmp/images.txt
cp -a /opt/precache /host/tmp/
cp -rf /etc/config /host/tmp/precache/config
# only check space for OCP upgrade
if [ -n "$(cat /etc/config/platform.image)" ]; then
    /opt/precache/check_space
    [ $? -ne 0 ] && echo "not enough space for precaching" > /dev/stderr && exit 17
fi
chroot /host /tmp/precache/release
chroot /host /tmp/precache/olm
chroot /host /tmp/precache/pull
