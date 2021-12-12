#!/bin/bash
# Unit tests: from current directory, run "cwd=. ./test.sh"
set -e

fatal() {
  echo "FATAL: $@"
  exit 1
}

for f in common olm release pull
do
    echo "Testing import of $f"
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
mkdir -p /tmp/release-manifests
printf '{
  "spec": {
    "tags": [
      {
        "from": {
          "name": "test"
        }
      }]
  }
}' > /tmp/release-manifests/image-references
(rm $pull_spec_file || true) &> /dev/null

container_tool=/usr/bin/echo
config_volume_path=/tmp

# Test common
echo "Testing common functions:"

result=$(pull_index "temp" $pull_secret_path)
[[ $? -eq 0 ]] || fatal "pull_index unexpected exit code"
[[ $result == "pull --quiet temp --root=/cache --authfile=$pull_secret_path" ]] || fatal "Index pull failure"
echo " Index pull pass"

result=$(mount_index test)
[[ $? -eq 0 ]] || fatal "mount_index unexpected exit code"
[[ $result == "--root=/cache image mount test" ]]  || fatal "Index image mount failure"
echo " Index image mount pass"

result=$(unmount_index test)
[[ $? -eq 0 ]] || fatal "mount_index_image unexpected exit code"
[[ $result == "--root=/cache image unmount test" ]]  || fatal "Index image unmount failure"
echo " Index image unmount pass"

# Test olm
echo "Testing olm unit:"
result=$(extract_packages)
[[ $result == "package1,package2" ]]  || fatal "Package name extraction failure"
echo " extract_packages - OK"

# Test release
echo "Testing release unit:"
result=$(extract_pull_spec "/tmp")
[[ $? -eq 0 ]] || fatal "release_image extract unexpected exit code"
[[ $(cat $pull_spec_file) == "\"test\"" ]] || fatal "release pull spec extract failure"
echo " release extract_pull_spec pass"

# Clean
rm -rf /tmp/operators.indexes /tmp/release-manifests $pull_spec_file /tmp/operators.packagesAndChannels
