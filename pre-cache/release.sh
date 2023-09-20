#!/bin/bash

cwd=$(dirname "$0")
# shellcheck source=pre-cache/common.sh
. $cwd/common.sh

extract_pull_spec(){
    local rel_img_mount=$1

    # remove empty lines
    sed -i '/^[[:space:]]*$/d' $CONFIG_VOLUME_PATH/excludePrecachePatterns
    # remove trailing and leading whitespace
    sed -i 's/^[ \t]*//;s/[ \t]*$//' $CONFIG_VOLUME_PATH/excludePrecachePatterns
    
    jq  '.spec.tags[] | .name as $name |.from.name as $pull |[$name,$pull] |join("$")' < ${rel_img_mount}/release-manifests/image-references | \
       grep -vG -f $CONFIG_VOLUME_PATH/excludePrecachePatterns | \
       cut -d "$" -f2 | \
       sed 's/^/"/' >> $PULL_SPEC_FILE
    log_debug "Release index image processing done"
}

release_main(){
    rel_img=$(cat $CONFIG_VOLUME_PATH/platform.image)
    if [[ -z $rel_img ]]; then
      log_debug "Release index is not specified. Release images will not be pre-cached"
      return 0
    fi
    release_index_id=$(pull_index $rel_img $PULL_SECRET_PATH)
    [[ $? -eq 0 ]] || return 1
    rel_img_mount=$(mount_index $release_index_id)
    [[ $? -eq 0 ]] || return 1
    extract_pull_spec $rel_img_mount
    [[ $? -eq 0 ]] || return 1
    unmount_index $release_index_id
    [[ $? -eq 0 ]] || return 1
    return 0
}

if [[ "${BASH_SOURCE[0]}" = "${0}" ]]; then
  release_main
  exit $?
fi
