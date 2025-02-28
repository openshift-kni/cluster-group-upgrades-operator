#!/usr/bin/env bash

while read line
do
    read -ra array <<<"${line}"
    sed -i "s,${array[0]},${array[1]},g" /manifests/cluster-group-upgrades-operator.clusterserviceversion.yaml
done < konflux_clusterserviceversion_overlay.data
