#!/bin/bash

CONTAINER_TOOL="${container_tool:-podman}"
PULL_SECRET_PATH="${pull_secret_path:-/var/lib/kubelet/config.json}"
export PULL_SPEC_FILE="${pull_spec_file:-/tmp/images.txt}"
CONFIG_VOLUME_PATH="${CONFIG_VOLUME_PATH:-/tmp/precache/config}"
export ADDITIONAL_IMAGES_SPEC_FILE="${additional_images_spec_file:-${CONFIG_VOLUME_PATH}/additionalImages}"

if ! [[ $TEST_ENV ]]; then
# This fixes process substitution issues in chroot
    ln -snf /proc/self/fd /dev/fd
fi

# LOGLEVELS: [0]ERROR, [1]INFO, [2]DEBUG
LOGLEVEL=${PRE_CACHE_LOG_LEVEL:-2} # set default log level to DEBUG

_log() {
    local level=$1; shift
    if [[ $level -le $LOGLEVEL ]]; then
        echo "upgrades.pre-cache $(date -Iseconds) $*" >&2
    fi
}

log_error() {
    _log 0 "[ERROR]: $*"
}

log_info() {
    _log 1 "[INFO]: $*"
}

log_debug() {
    _log 2 "[DEBUG]: $*"
}

pull_index(){
    local index_pull_spec=$1
    local PULL_SECRET_PATH=$2
    # Pull the image into the cache directory and attain the image ID
    release_index_id=$($CONTAINER_TOOL pull --quiet  $index_pull_spec --authfile=$PULL_SECRET_PATH)
    [[ $? -eq 0 ]] || return 1
    echo $release_index_id
    return 0
}

mount_index(){
    local image_id=$1
    local image_mount
    image_mount=$($CONTAINER_TOOL image mount $image_id)
    rv=$?
    echo $image_mount
    return $rv
}

unmount_index(){
    local image_id=$1
    local result
    result=$($CONTAINER_TOOL image unmount $image_id)
    rv=$?
    echo $result
    return $rv
}

#
# with_retries:
# Helper function to run a command with retries on failure
#
function with_retries {
    local max_attempts=$1
    local delay=$2
    local cmd=("${@:3}")
    local attempt=0
    local rc=0

    while [[ ${attempt} -lt ${max_attempts} ]]; do
        attempt=$((attempt+=1))
        if [[ ${attempt} -gt 1 ]]; then
            log_debug "Retrying after ${delay} seconds, attempt #${attempt}"
            sleep "${delay}"
        fi

        "${cmd[@]}"
        rc=$?
        if [ $rc -eq 0 ]; then
            log_debug "Command succeeded:" "${cmd[@]}"
            break
        fi

        log_error "Command failed:" "${cmd[@]}"
    done

    return ${rc}
}
