#!/usr/bin/env bash

# Check if we should apply Konflux overlay or not
if [[ ${KONFLUX} == true ]]; then
    echo "Overlaying Konflux pinning to clusterserviceversion"

    # Loop over the input data
    while read -r line; do
        # Split the line into two over the space character
        # array[0] is the old string to be replaced
        # array[1] is the new string
        read -ra array <<<"${line}"

        # Use sed to perform the replacement globally on the clusterserviceversion yaml file
        sed -i "s,${array[0]},${array[1]},g" /manifests/cluster-group-upgrades-operator.clusterserviceversion.yaml
    done < konflux_clusterserviceversion_overlay.data
else
    echo "KONFLUX was not set to true"
fi
