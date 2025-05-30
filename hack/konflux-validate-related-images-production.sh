#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# set -x

SCRIPT_NAME=$(basename "$(readlink -f "${BASH_SOURCE[0]}")")

check_preconditions() {
    echo "Checking pre-conditions..."

    # yq must be installed
    command -v yq >/dev/null 2>&1 || { echo "Error: yq seems not to be installed." >&2; exit 1; }

    echo "Checking pre-conditions completed!"
    return 0
}

parse_args() {
    echo "Parsing args..."

    # command line options
    local options=
    local long_options="set-catalog-file:,help"
    local parsed
    parsed=$(getopt --options="$options" --longoptions="$long_options" --name "$SCRIPT_NAME" -- "$@")
    eval set -- "$parsed"

    declare -g ARG_CATALOG_FILE=""

    while true; do
        case $1 in
            --help)
                usage
                exit
                ;;
            --set-catalog-file)
                if [ -z "$2" ]; then
                    echo "Error: --catalog-file requires a file " >&2;
                    exit 1;
                fi

                ARG_CATALOG_FILE=$2
                if [[ ! -f "$ARG_CATALOG_FILE" ]]; then
                    echo "Error: file '$ARG_CATALOG_FILE' does not exist." >&2
                    exit 1
                fi

                shift 2
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

    if [ -z "$ARG_CATALOG_FILE" ]; then
        echo "Error: --set-catalog--file is required" >&2
        exit 1
    fi

    echo "Parsing args completed!"
    return 0
}

validate_related_images() {
    echo "Validating related images..."

    # validate .entries exists
    if ! yq e '.relatedImages | type == "!!seq"' "$ARG_CATALOG_FILE" >/dev/null; then
        echo "Error: .entries in $ARG_CATALOG_FILE is not a valid array." >&2
        exit 1
    fi

    local images_parsed
    mapfile -t images_parsed < <(yq eval '.relatedImages | .[] | .image' "$ARG_CATALOG_FILE")
    entries=${#images_parsed[@]}

    declare -i i=0
    for ((; i<entries; i++)); do
        local image="${images_parsed[i]}"
        # Only allow production (registry.redhat.io) images
        if [[ "$image" =~ ^registry\.redhat\.io/ ]]; then
            echo "Valid production image found: $image"
        else
            echo "Error: $image is not a valid image reference for production. Check bundle overlay." >&2
            exit 1
        fi
    done

    echo "Validating related images completed!"
    return 0
}


main() {
    check_preconditions
    parse_args "$@"
    validate_related_images
}

usage() {
   cat << EOF
NAME

   $SCRIPT_NAME - check the relatedImages section on a catalog yaml file is suitable for production

SYNOPSIS

   $SCRIPT_NAME --set-catalog-file FILE

EXAMPLES

   - Check the catalog template 'catalog.yaml'

     $ $SCRIPT_NAME --set-catalog-file catalog.yaml

DESCRIPTION

ARGS

   --set-catalog-file FILE
      Set the catalog file.

   --help
      Display this help and exit.
EOF
}

main "$@"
