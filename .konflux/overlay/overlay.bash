#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

#set -x
# Debug mode off by default
DEBUG=false

SCRIPT_DIR=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
SCRIPT_NAME=$(basename "$(readlink -f "${BASH_SOURCE[0]}")")

MAP_STAGING="staging"
MAP_PRODUCTION="production"
MANAGER_KEY="manager"

debug() {
    if [ "$DEBUG" = true ]; then
        echo "[DEBUG] $1"
    fi
}

check_preconditions() {
    echo "Checking pre-conditions..."

    # yq must be installed
    command -v yq >/dev/null 2>&1 || { echo "Error: yq seems not to be installed." >&2; exit 1; }
    echo "Checking pre-conditions completed!"
    return 0
}

pin_images() {
    echo "Pinning images (sha256)..."

    for image_name in "${!IMAGE_TO_SOURCE[@]}"; do
        echo "Replacing: image_name: $image_name, source: ${IMAGE_TO_SOURCE[$image_name]}, target: ${IMAGE_TO_TARGET[$image_name]}"
        sed -i "s,${IMAGE_TO_SOURCE[$image_name]},${IMAGE_TO_TARGET[$image_name]},g" $ARG_CSV_FILE
    done

    echo "Pinning images completed!"
    return 0
}

add_related_images() {
    echo "Adding related images..."

    # remove the existing section
    echo "Removing .spec.relatedImages"
    yq e -i 'del(.spec.relatedImages)' $ARG_CSV_FILE

    # create a new section from scratch
    declare -i index=0
    for image_name in "${!IMAGE_TO_SOURCE[@]}"; do
        echo "Adding related image: name: $image_name source: ${IMAGE_TO_SOURCE[$image_name]}, image: ${IMAGE_TO_TARGET[$image_name]}"
        yq e -i ".spec.relatedImages[$index].name=\"$image_name\" |
                 .spec.relatedImages[$index].image=\"${IMAGE_TO_TARGET[$image_name]}\"" $ARG_CSV_FILE
        index=$index+1
    done

    echo "Adding related images completed!"
    return 0
}

parse_mapping_images_file() {
    echo "Parsing mapping image file..."

    # Extract keys and images
    local keys=($(yq eval '.[].key' "$ARG_MAPPING_FILE"))
    local staging_images=($(yq eval '.[].staging' "$ARG_MAPPING_FILE"))
    local production_images=($(yq eval '.[].production' "$ARG_MAPPING_FILE"))
    local entries=${#keys[@]}

    # Declare associative arrays
    declare -gA IMAGE_TO_STAGING=()
    declare -gA IMAGE_TO_PRODUCTION=()

    declare -i i=0
    for ((; i<entries; i++)); do
        # Store in associative arrays
        local key=${keys[i]}
        IMAGE_TO_STAGING["$key"]="${staging_images[i]}"
        IMAGE_TO_PRODUCTION["$key"]="${production_images[i]}"
    done

    echo "Parsing mapping image file completed!"
    return 0
}

map_images() {

    if [[ ! -f "$ARG_MAPPING_FILE" ]]; then
        echo "Skipping images mapping!"
        return 0
    fi

    echo "Mapping images ..."

    parse_mapping_images_file

    for image_name in "${!IMAGE_TO_TARGET[@]}"; do
        local image_name_target="${IMAGE_TO_TARGET[$image_name]}"

        # requires an image already pinned, sha256 format: '...@sha256:..."
        local image_name_target_trimmed="${image_name_target%@*}"

        local image_name_target_trimmed_mapped=""
        if [[ "$ARG_MAP" == "$MAP_STAGING" ]]; then
            if [[ -z "${IMAGE_TO_STAGING[$image_name]-}" ]]; then
                echo "Warning: no staging image mapped for: $image_name" >&2
                continue
            fi

            image_name_target_trimmed_mapped="${IMAGE_TO_STAGING[$image_name]}"

        elif [[ "$ARG_MAP" == "$MAP_PRODUCTION" ]]; then
            if [[ -z "${IMAGE_TO_PRODUCTION[$image_name]-}" ]]; then
                echo "Warning: no production image mapped for: $image_name" >&2
                continue
            fi

            image_name_target_trimmed_mapped="${IMAGE_TO_PRODUCTION[$image_name]}"

        fi

        echo "Replacing: image_name: $image_name, original: $image_name_target_trimmed, mapped: $image_name_target_trimmed_mapped"
        sed -i "s,$image_name_target_trimmed,$image_name_target_trimmed_mapped,g" $ARG_CSV_FILE
    done

    echo "Mapping images completed"
}

parse_pinning_images_file() {
    echo "Parsing pinning file..."

    if [[ ! -f "$ARG_PINNING_FILE" ]]; then
        echo "Error: File '$ARG_PINNING_FILE' not found. " >&2
        exit 1
    fi

    # Extract keys and images
    local keys=($(yq eval '.[].key' "$ARG_PINNING_FILE"))
    local sources=($(yq eval '.[].source' "$ARG_PINNING_FILE"))
    local targets=($(yq eval '.[].target' "$ARG_PINNING_FILE"))
    local entries=${#keys[@]}

    # Declare associative arrays
    declare -gA IMAGE_TO_SOURCE=()
    declare -gA IMAGE_TO_TARGET=()

    declare -i i=0
    for ((; i<entries; i++)); do
        # Store in associative arrays
        local key=${keys[i]}
        IMAGE_TO_SOURCE["$key"]="${sources[i]}"
        IMAGE_TO_TARGET["$key"]="${targets[i]}"
    done

    if [ "$DEBUG" = true ]; then
        for key in "${!IMAGE_TO_SOURCE[@]}"; do
            echo "- key: $key"
            echo "  source: ${IMAGE_TO_SOURCE[$name]}"
            echo "  target: ${IMAGE_TO_TARGET[$name]}"
        done
    fi

    echo "Parsing pinning file completed!"
    return 0
}

parse_args() {
    echo "Parsing args..."

    # command line options
    local options=
    local long_options="set-pinning-file:,set-mapping-file:,set-csv-file:,set-mapping-staging,set-mapping-production,help"

    local parsed=$(getopt --options="$options" --longoptions="$long_options" --name "$SCRIPT_NAME" -- "$@")
    eval set -- "$parsed"

    local map_staging=0
    local map_production=0
    declare -g ARG_MAPPING_FILE=""
    declare -g ARG_PINNING_FILE=""
    declare -g ARG_CSV_FILE=""
    declare -g ARG_MAP=""
    while true; do
        case $1 in
            --help)
                usage
                exit
                ;;
            --set-csv-file)
                ARG_CSV_FILE=$2
                shift 2
                ;;
            --set-pinning-file)
                ARG_PINNING_FILE=$2
                shift 2
                ;;
            --set-mapping-file)
                ARG_MAPPING_FILE=$2
                shift 2
                ;;
            --set-mapping-staging)
                map_staging=1
                ARG_MAP=$MAP_STAGING
                shift 1
                ;;
            --set-mapping-production)
                map_production=1
                ARG_MAP=$MAP_PRODUCTION
                shift 1
                ;;
            --)
                shift
                break
                ;;
            *)
                echo "Error: unexpected option: $1" >&2
                usage
                exit 1
                ;;
        esac
    done

    # validate images file
    if [[ -n $ARG_PINNING_FILE && ! -f "$ARG_PINNING_FILE" ]]; then
        echo "Error: file '$ARG_PINNING_FILE' does not exist." >&2
        exit 1
    fi

    # validate csv file
    if [[ -n $ARG_CSV_FILE && ! -f "$ARG_CSV_FILE" ]]; then
        echo "Error: file '$ARG_CSV_FILE' does not exist." >&2
        exit 1
    fi

    # validate map options
    if [[ $map_staging -eq 1 && $map_production -eq 1 ]]; then
        echo "Error: cannot specify both '--set-mapping-staging' and '--set-mapping-production'." >&2
        exit 1
    fi

    if [[ $map_staging -eq 1 || $map_production -eq 1 ]]; then
        if [[ ! -n $ARG_MAPPING_FILE ]]; then
            echo "Error: specify '--set-mapping-file' to use a container registry map file." >&2
            exit 1
        fi

        if [[ ! -f "$ARG_MAPPING_FILE" ]]; then
            echo "Error: file '$ARG_MAPPING_FILE' does not exist." >&2
            exit 1
        fi
    fi

    if [[ -n $ARG_MAPPING_FILE ]]; then
        if [[ $map_staging -eq 0 && $map_production -eq 0 ]]; then
            echo "Error: specify '--set-mapping-staging' or '--set-mapping-production'." >&2
            exit 1
        fi
    fi

    echo "Parsing args completed!"
}

overlay_release()
{
    echo "Overlaying release..."

    local display_name="Topology Aware Lifecycle Manager"
    local description="Topology Aware Lifecycle Manager is an operator that facilitates
      platform and operator upgrades of group of clusters"
    local version="4.15.1"
    local name="topology-aware-lifecycle-manager"
    local name_version="$name.v$version"
    local manager="topology-aware-lifecycle-manager"
    local skip_range=">=4.9.0 <4.15.1"
    local replaces="topology-aware-lifecycle-manager.v4.15.0"
    # min_kube_version should match ocp
    # https://access.redhat.com/solutions/4870701
    export min_kube_version="1.28.0"

    yq e -i ".metadata.annotations[\"containerImage\"] = \"${IMAGE_TO_TARGET[$MANAGER_KEY]}\"" $ARG_CSV_FILE
    yq e -i ".spec.displayName = \"$display_name\"" $ARG_CSV_FILE
    yq e -i ".spec.description = \"$description\""  $ARG_CSV_FILE
    yq e -i ".spec.version = \"$version\"" $ARG_CSV_FILE
    yq e -i ".metadata.name = \"$name_version\"" $ARG_CSV_FILE
    yq e -i ".metadata.annotations[\"olm.skipRange\"] = \"$skip_range\"" $ARG_CSV_FILE
    yq e -i ".spec.minKubeVersion = \"$min_kube_version\"" $ARG_CSV_FILE

    # dont need 'replaces' for first release in a new channel (4.15.0)
    # yq e -i "del(.spec.replaces)" $ARG_CSV_FILE

    # use this from 4.15.1 onwards
    yq e -i ".spec.replaces = \"$replaces\"" $ARG_CSV_FILE

    echo "Overlaying release completed!"
}

main() {
   check_preconditions
   parse_args "$@"
   parse_pinning_images_file
   pin_images
   add_related_images
   overlay_release
   map_images    # this MUST always be the last action
}

usage() {
   cat << EOF
NAME

   $SCRIPT_NAME - overlay operator csv

SYNOPSIS

   $SCRIPT_NAME --set-pinning-file FILE [--set-mapping-file FILE (--set-mapping-staging|--set-mapping-production) --set-csv-file FILE

EXAMPLES

   - Pin (sha256) images on 'oran-o2ims.clusterserviceversion.yaml' according to the configuration on 'pin_images.in.yaml':

     $ $SCRIPT_NAME --set-pinning-file pin_images.in.yaml --set-csv-file oran-o2ims.clusterserviceversion.yaml

   - Pin (sha256) images on 'oran-o2ims.clusterserviceversion.yaml' according to the configuration on 'pin_images.in.yaml'
     and map them to the production registry according to the configuration on 'map_images.in.yaml':

     $ $SCRIPT_NAME --set-pinning-file pin_images.in.yaml --set-mapping-file map_images.in.yaml --set-mapping-production --set-csv-file oran-o2ims.clusterserviceversion.yaml

DESCRIPTION

   overlay operator csv

ARGS

   --set-pinning-file FILE
      Set the pinning file to pin image refs to sha256

   --set-mapping-file FILE
      Set the mapping file to map image refs to another container registry

      When used, it must be accompanied by either:

        --set-mapping-staging    map to 'registry.stage.redhat.io'
        --set-mapping-production map to 'registry.redhat.io'

   --set-csv-file FILE
      Set the cluster service version file

   --help
      Display this help and exit.

EOF
}

main "$@"
