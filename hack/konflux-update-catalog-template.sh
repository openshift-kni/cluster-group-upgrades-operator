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
    local long_options="set-catalog-template-file:,set-bundle-builds-file:,help"
    local parsed
    parsed=$(getopt --options="$options" --longoptions="$long_options" --name "$SCRIPT_NAME" -- "$@")
    eval set -- "$parsed"

    declare -g ARG_CATALOG_TEMPLATE_FILE=""
    declare -g ARG_BUNDLE_BUILDS_FILE=""

    while true; do
        case $1 in
            --help)
                usage
                exit
                ;;
            --set-catalog-template-file)
                if [ -z "$2" ]; then
                    echo "Error: --catalog-template-file requires a file " >&2;
                    exit 1;
                fi

                ARG_CATALOG_TEMPLATE_FILE=$2
                if [[ ! -f "$ARG_CATALOG_TEMPLATE_FILE" ]]; then
                    echo "Error: file '$ARG_CATALOG_TEMPLATE_FILE' does not exist." >&2
                    exit 1
                fi

                shift 2
                ;;
            --set-bundle-builds-file)
                if [ -z "$2" ]; then
                    echo "Error: --set-bundle-builds-file requires a file " >&2;
                    exit 1;
                fi

                ARG_BUNDLE_BUILDS_FILE=$2
                if [[ ! -f "$ARG_BUNDLE_BUILDS_FILE" ]]; then
                    echo "Error: file '$ARG_BUNDLE_BUILDS_FILE' does not exist." >&2
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

    # Ensure both files are provided
    if [ -z "$ARG_CATALOG_TEMPLATE_FILE" ]; then
        echo "Error: --set-catalog-template-file is required" >&2
        exit 1
    fi

    if [ -z "$ARG_BUNDLE_BUILDS_FILE" ]; then
        echo "Error: --set-bundle-builds-file is required" >&2
        exit 1
    fi

    echo "Parsing args completed!"
    return 0
}

validate_catalog_template_file() {
    echo "Validating catalog template file..."

    # validate .entries exists
    if ! yq e '.entries | type == "!!seq"' "$ARG_CATALOG_TEMPLATE_FILE" >/dev/null; then
        echo "Error: .entries in $ARG_CATALOG_TEMPLATE_FILE is not a valid array." >&2
        exit 1
    fi

    # validate the last entry has an image field
    image_field=$(yq e ".entries[-1].image" "$ARG_CATALOG_TEMPLATE_FILE")
    if [ -z "$image_field" ] || [ "$image_field" = "null" ]; then
        echo "Error: Last element in .entries array of $ARG_CATALOG_TEMPLATE_FILE is missing the image field or it is null." >&2
    fi

    echo "Validating catalog template file completed!"
    return 0
}

update_catalog_template_file() {
    echo "Updating catalog template file..."

    # Extract bundle
    local bundle_quay
    bundle_quay="$(yq eval '.quay' "$ARG_BUNDLE_BUILDS_FILE")"
    if [ -z "$bundle_quay" ] || [ "$bundle_quay" = "null" ]; then
        echo "Error: No .quay key found in $ARG_BUNDLE_BUILDS_FILE or value is null." >&2
        exit 1
    fi

    # Override the last entry with the quay build
    yq e -i ".entries[-1].image = \"$bundle_quay\"" $ARG_CATALOG_TEMPLATE_FILE
    echo "Updating catalog template file: $ARG_CATALOG_TEMPLATE_FILE with bundle: $bundle_quay"

    echo "Updating catalog template file completed!"
    return 0
}

main() {
    check_preconditions
    parse_args "$@"
    validate_catalog_template_file
    update_catalog_template_file
}

usage() {
   cat << EOF
NAME

   $SCRIPT_NAME - update a catalog template based on the bundle builds to be included

SYNOPSIS

   $SCRIPT_NAME --set-catalog-template-file FILE --set-bundle-builds-file FILE

EXAMPLES

   - Update the catalog template '.konflux/catalog/catalog-template.in.yaml' based on the bundles builds on 'bundle.builds.in.yaml':

     $ $SCRIPT_NAME --set-catalog-template-file .konflux/catalog/catalog-template.in.yaml --set-bundle-builds-file .konflux/catalog/bundle.builds.in.yaml

DESCRIPTION

ARGS

   --set-catalog-template-file FILE
      Set the catalog template file.

   --set-bundle-builds-file FILE
      Set the bundle builds file.

   --help
      Display this help and exit.
EOF
}

main "$@"
