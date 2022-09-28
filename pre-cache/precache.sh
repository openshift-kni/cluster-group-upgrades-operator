#!/bin/bash

set -e

rm -rf /host/tmp/precache
cp -a /opt/precache /host/tmp/
cp -rf /etc/config /host/tmp/precache/config
chroot /host /tmp/precache/release
chroot /host /tmp/precache/olm
chroot /host /tmp/precache/pull
