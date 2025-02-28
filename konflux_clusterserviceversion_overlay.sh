#!/usr/bin/env bash

if [[ ${KONFLUX} == true ]]; then
    echo "Overlaying Konflux pinning to clusterserviceversion"
    while read line
    do
        read -ra array <<<"${line}"
        sed -i "s,${array[0]},${array[1]},g" /manifests/cluster-group-upgrades-operator.clusterserviceversion.yaml
    done < konflux_clusterserviceversion_overlay.data
else
    echo "KONFLUX was not set to true"
fi
