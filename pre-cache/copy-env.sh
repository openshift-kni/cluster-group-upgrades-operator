#!/bin/bash

# Copy podman dependencies to the container. Podman itself runs off the host in the
# container by adding /host/usr/bin to the path
# This is a work around for podman not passing LD_LIBRARY_PATH environment
#   variable to other binaries it calls
declare -a libs=("libseccomp.so*" "libgpgme.so*" "libassuan.so*" "libgpg-error.so*" 
                  "libglib-2.0.so*" "libsystemd.so*" "libgcc_s.so*" "libgnutls.so*"
                  "libpcre.so*" "liblzma.so*" "liblz4.so*" "libmount.so*" "libgcrypt.so*"
                  "libp11-kit.so*" "libidn2.so*" "libunistring.so*" "libtasn1.so*" 
                  "libnettle.so*" "libhogweed.so*" "libgmp.so*" "libblkid.so*" "libuuid.so*"
                  "libffi.so*" )

for lib in "${libs[@]}"
do
   cp /host/usr/lib64/$lib /usr/lib64/
done

# Copy containers policy and cacert of disconnected registries if present
cp /host/etc/containers/policy.json /etc/containers/policy.json
cp -a /host/etc/docker /etc/ || true

