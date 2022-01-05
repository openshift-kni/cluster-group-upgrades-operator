#!/bin/bash

set -e

# Pull spec extraction is done in the container due to opm<->selinux issues
/opt/precache/copy-env.sh
/opt/precache/release
/opt/precache/olm 

# Image pull is done on the host using "chroot /host"
cp /tmp/images.txt /host/tmp/
rm -rf /host/tmp/precache
cp -a /opt/precache /host/tmp/
chroot /host /tmp/precache/pull
