#!/bin/bash

cwd="${cwd:-/tmp/precache}"
# shellcheck source=pre-cache/common.sh
. $cwd/common.sh

wait_image(){
    local img=$1
    local pid=$2

    log_debug "Waiting to finish pulling image: ${img}"
    wait $pid # The way wait monitor for each background task (PID). If any error then copy the image in the failed array so it can be retried later
    if [[ $? != 0 ]]; then
        log_error "Pull failed for image: ${img}! Will retry later... "
        failed_pulls+=("${img}") # Failed, then add the image to be retrieved later
    fi
}

mirror_images() {
    local pull_file=$1
    local pull_type=$2

    if ! [[ -f $pull_file ]]; then
        log_error "No pull spec provided for ${pull_type}"
        return 1
    fi

    max_pull_threads="${MAX_PULL_THREADS:-10}" # number of simultaneous pulls executed can be modified by setting MAX_PULL_THREADS environment variable
    # definition of vars once the config is provided
    local max_bg=$max_pull_threads
    declare -A pids # Hash that include the images pulled along with their pids to be monitored by wait command
    local total_pulls
    total_pulls=$(sort -u $pull_file | wc -l)  # Required to keep track of the pull task vs total
    local current_pull=1

    # for line in $(sort -u $pull_file); do
    while IFS= read -r line; do
        # Strip double quotes
        img="${line%\"}"
        img="${img#\"}"
        log_debug "Pulling ${img} [${current_pull}/${total_pulls}]"
        # If image is on disk, then skip. This improves the global performance
        $CONTAINER_TOOL image exists $img
        if [[ $? == 0 ]]; then
            log_debug "Skipping existing image $img"
            current_pull=$((current_pull + 1))
            continue
        fi
        $CONTAINER_TOOL pull $img --authfile=$PULL_SECRET_PATH -q > /dev/null &
        #$CONTAINER_TOOL copy docker://${img} --authfile=/var/lib/kubelet/config.json containers-storage:${img} -q & # SKOPEO 
        pids[${img}]=$! # Keeping track of the PID and container image in case the pull fails
        max_bg=$((max_bg - 1)) # Batch size adapted 
        current_pull=$((current_pull + 1))
        if [[ $max_bg == 0 ]]; then # If the batch is done, then monitor the status of all pulls before moving to the next batch
            for pid in "${!pids[@]}"; do
                wait_image $pid ${pids[$pid]}
            done
            # Once the batch is processed, reset the new batch size and clear the processes hash for the next one
            max_bg=$max_pull_threads
            pids=()
        fi
    done < $pull_file

    # Make sure that all pull-threads have been processed
    for pid in "${!pids[@]}"; do
        wait_image $pid ${pids[$pid]}
    done
}

retry_images() {
    local pull_type=$1
    local success
    local iterations
    local rv=0

    for failed_pull in "${failed_pulls[@]}"; do
        success=0
        iterations=10
        until [[ $success -eq 1 ]] || [[ $iterations -eq 0 ]]; do
            log_info "Retrying failed image pull: ${failed_pull}"
            $CONTAINER_TOOL pull $failed_pull --authfile=$PULL_SECRET_PATH
            if [[ $? == 0 ]]; then
                success=1
            fi
            iterations=$((iterations - 1))
        done
        if [[ $success == 0 ]]; then
            log_error "Limit number of retries reached. The image  ${failed_pull} could not be pulled."
            rv=1
        fi
    done
    return $rv
}

pre_cache_images(){
    local pull_file=$1
    local pull_type=$2

    log_info "Image pre-caching starting for ${pull_type}"

    failed_pulls=() # Clear the failed_pull array
    mirror_images $pull_file $pull_type
    [[ $? -eq 0 ]] || return 1

    retry_images $pull_type # Return 1 if max.retries reached
    if [[ $? -ne 0 ]]; then
        log_error "One or more images were not pre-cached successfully for ${pull_type}"
        return 1
    fi

    log_info "Image pre-caching complete for ${pull_type}"
    return 0
}

if [[ "${BASH_SOURCE[0]}" = "${0}" ]]; then
    declare -A pull_types
    pull_types["platform-images"]=${PULL_SPEC_FILE}
    pull_types["additional-images"]=${ADDITIONAL_IMAGES_SPEC_FILE}

    failed_pulls=() # Array that will include all the images that failed to be pulled

    for pull_type in "${!pull_types[@]}"; do
        pull_file=${pull_types[$pull_type]}
        # Check if the spec_file exists before executing pull statements
        if [ -n "$(cat $pull_file)" ]; then
                pre_cache_images $pull_file $pull_type
                [[ $? -eq 0 ]] || exit 1
        fi
    done
    exit 0
fi
