#!/bin/bash
# Unit tests: from current directory, run "cwd=. ./test.sh"
set -e

fatal() {
    echo "FATAL: $*"
    exit 1
}

for f in common.sh olm.sh release.sh pull.sh; do
    echo "Testing import of $f"
    # shellcheck disable=1090,2154
    . $cwd/$f
    rc=$?
    [[ $rc -eq 0 ]] || fatal "Could not import $f"
    echo "Ok"
done

# Mock
echo "test_index1" > /tmp/operators.indexes
echo "test_index2" >> /tmp/operators.indexes
echo "  package1:  channel1 " > /tmp/operators.packagesAndChannels
echo "  package2:channel2" >> /tmp/operators.packagesAndChannels
# the script should ignore traling and leading whitespaces and empty lines
echo -e "\n\n aws \n alibaba \n" > /tmp/excludePrecachePatterns

mkdir -p /tmp/release-manifests
cat <<EOF > /tmp/release-manifests/image-references
{
  "spec": {
    "tags": [
      {
        "name": "redhat",
        "from": {
          "name": "quay.io/1"
        }
      },
      {
        "name": "bawsa",
        "from": {
          "name": "quay.io/2"
        }
      }
      ]
  }
}
EOF

# shellcheck disable=SC2154
(rm $PULL_SPEC_FILE || true) &> /dev/null

# shellcheck disable=SC2034
CONTAINER_TOOL=/usr/bin/echo
# shellcheck disable=SC2034
CONFIG_VOLUME_PATH=/tmp

# Test common
echo "Testing common functions:"

# shellcheck disable=SC2154
result=$(pull_index "temp" $PULL_SECRET_PATH)
[[ $? -eq 0 ]] || fatal "pull_index unexpected exit code"
[[ $result == "pull --quiet temp --authfile=$PULL_SECRET_PATH" ]] || fatal "Index pull failure"
echo " Index pull pass"

result=$(mount_index test)
[[ $? -eq 0 ]] || fatal "mount_index unexpected exit code"
[[ $result == "image mount test" ]]  || fatal "Index image mount failure"
echo " Index image mount pass"

result=$(unmount_index test)
[[ $? -eq 0 ]] || fatal "mount_index_image unexpected exit code"
[[ $result == "image unmount test" ]]  || fatal "Index image unmount failure"
echo " Index image unmount pass"

# Test olm
echo "Testing olm unit:"
result=$(extract_packages)
[[ $result == "package1,package2" ]]  || fatal "Package name extraction failure"
echo " extract_packages - pass"

# Test release
echo "Testing release unit:"
result=$(extract_pull_spec "/tmp")
[[ $? -eq 0 ]] || fatal "release_image extract unexpected exit code"
[[ $(cat $PULL_SPEC_FILE) == "\"quay.io/1\"" ]] || fatal "release pull spec extract failure"
echo " release extract_pull_spec pass"

# Clean
rm -rf /tmp/operators.indexes /tmp/release-manifests $PULL_SPEC_FILE /tmp/operators.packagesAndChannels
