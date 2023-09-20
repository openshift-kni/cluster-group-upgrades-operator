#!/bin/bash

cwd=$(dirname "$0")
# shellcheck source=pre-cache/common.sh
. $cwd/common.sh

rendered_index_path="${rendered_index_path:-/tmp/index.json}"

extract_pull_spec(){
    rendered_index=$1
    operators_spec_file=$2
    PULL_SPEC_FILE=$3
    /usr/libexec/platform-python "${cwd}/parse_index.py" "${rendered_index}" "${operators_spec_file}" "${PULL_SPEC_FILE}"
    return $?
}

render_index(){
    index=$1
    packages=$2
    image_mount=$3
    
    if [[ -h "$image_mount/bin/opm" ]]; then
        opm_bin=$image_mount$(readlink "$image_mount/bin/opm")
    else
        opm_bin=$image_mount/bin/opm
    fi

    if [[ -d "$image_mount/configs" ]]; then
      log_debug "Rendering file based catalog"
      $opm_bin render "$image_mount/configs" > $rendered_index_path
    else
      log_debug "Rendering SQLite catalog"
      $opm_bin render "$image_mount/database/index.db" > $rendered_index_path
    fi
    if [[ $? -ne 0 ]]; then
        return 1
    fi

}

extract_packages(){
    local packages
    tr -d " " < $CONFIG_VOLUME_PATH/operators.packagesAndChannels > /tmp/packagesAndChannels
    while IFS= read -r item; do
    # for item in $(sort -u '/tmp/packagesAndChannels'); do
        pkg=$(echo $item |cut -d ':' -f 1)
        packages="$packages$pkg,"
    done < <(sort -u /tmp/packagesAndChannels)
    echo ${packages%,}
    return 0
}

olm_main(){
    if [ -z $(sort -u $CONFIG_VOLUME_PATH/operators.indexes) ]; then
      log_debug "Operators index is not specified. Operators won't be pre-cached"
      return 0
    fi
    # There could be several indexes, hence the loop
    sort -u $CONFIG_VOLUME_PATH/operators.indexes | while IFS= read -r index
    do
        image_id=$(pull_index $index $PULL_SECRET_PATH)
        if [[ $? -ne 0 ]]; then
          log_debug "pull_index failed for index $index"
          return 1
        fi
        log_debug "$index image ID is $image_id"

        image_mount=$(mount_index $image_id)
        if [[ $? -ne 0 ]]; then
          log_debug "mount_index failed for index $index"
          return 1
        fi
        log_debug "Image mount: $image_mount"

        packages=$(extract_packages)
        if [[ -z $packages ]]; then
          log_debug "operators index is set, but no packages provided - inconsistent configuration"
          return 1
        fi

        render_index $index $packages $image_mount
        if [[ $? -ne 0 ]]; then
          log_debug "render_index failed: OLM index render failed for index $index, package(s) $packages"
          return 1
        fi
        operators_spec_file="$CONFIG_VOLUME_PATH/operators.packagesAndChannels"
        extract_pull_spec $rendered_index_path $operators_spec_file $PULL_SPEC_FILE
        if [[ $? -ne 0 ]]; then
          log_debug "extract_pull_spec failed"
          return 1
        fi
        unmount_index $image_id
    done
    return 0
}

if [[ "${BASH_SOURCE[0]}" = "${0}" ]]; then
  olm_main
  exit $?
fi
