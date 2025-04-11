#!/usr/bin/env bash

set -eou pipefail

KONFLUX_DATA_FILE="/tmp/konflux_clusterserviceversion_overlay.data"
CLUSTER_SERVICE_VERSION_FILE="/tmp/manifests/cluster-group-upgrades-operator.clusterserviceversion.yaml"

function overlay_image_pinning {
    echo "Overlaying Konflux pinning to clusterserviceversion"

    # Loop over the input data
    while read -r line; do
        # Split the line into substrings over the space character
        # array[0] is the name of the image
        # array[1] is the old string to be replaced
        # array[2] is the new string
        read -ra array <<<"${line}"

        # Replace all old references with pinned ones
        echo "Replacing '${array[1]}' with '${array[2]}'"
        sed -i "s,${array[1]},${array[2]},g" "${CLUSTER_SERVICE_VERSION_FILE}"

    done < <(sed 's/#.*//' "${KONFLUX_DATA_FILE}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -v '^\s*$')

    echo "All replacements completed"
    echo ""
}

function add_related_images {
    echo "Adding related images to clusterserviceversion"

    # Generate yaml data into a file
    yaml_file="konflux_related_images_overlay.yaml"

    # Generate template file
    echo "spec:" > "${yaml_file}"
    echo "  customresourcedefinitions:" >> "${yaml_file}"
    echo "    relatedImages:" >> "${yaml_file}"

    # Loop over the input data
    while read -r line; do
        # Split the line into substrings over the space character
        # array[0] is the name of the image
        # array[1] is the old string to be replaced
        # array[2] is the new string
        read -ra array <<<"${line}"

        # Generate line by line data
        echo "      - image: \"${array[1]}\"" >> "${yaml_file}"
        echo "        name: ${array[0]}" >> "${yaml_file}"

    done < <(sed 's/#.*//' "${KONFLUX_DATA_FILE}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -v '^\s*$')

    echo "Generated file contents:"
    echo "---"
    cat ${yaml_file}
    echo "..."

    # use yq to combine overlay the related images data into the cluster service version file
    # we need to use a temp file here to prevent yq clobbering itself on the open file
    echo "Merging contents of '${yaml_file}' into '${CLUSTER_SERVICE_VERSION_FILE}'"
    # shellcheck disable=SC2016
    yq eval-all '. as $item ireduce ({}; . * $item )' "${CLUSTER_SERVICE_VERSION_FILE}" "${yaml_file}" > /tmp/output.yaml
    mv /tmp/output.yaml "${CLUSTER_SERVICE_VERSION_FILE}"

    # Check that we inserted more than 0 lines and that we didn't clobber the source file
    inserted_line_count=$(yq '.spec.customresourcedefinitions.relatedImages' < "${CLUSTER_SERVICE_VERSION_FILE}" | wc -l)
    total_line_count=$(wc -l < "${CLUSTER_SERVICE_VERSION_FILE}" )
    if [[ ! ${inserted_line_count} -gt 1 || $total_line_count -eq $inserted_line_count ]]; then
        echo "Failed to insert relatedImages data"
        return 1
    fi
    echo "Inserted ${inserted_line_count} lines"

    echo "Data merge completed"
    echo ""
}

function main {
    # The order is important here, we want to add related images first
    # Then replace all the images with the final pinned versions second
    echo ""
    add_related_images
    overlay_image_pinning
}

main
