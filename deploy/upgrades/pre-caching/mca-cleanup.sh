#!/bin/bash

spokes=( "spoke1" "spoke2" "spoke5" "spoke6" )

for spoke in "${spokes[@]}"; do
    echo "$spoke"
    mapfile -t mca < <(oc get managedclusteraction -n "$spoke" | tail -n +2 | awk '{print $1}')
    for (( i=0; i<${#mca[@]}; i++)); do
        oc delete managedclusteraction -n "$spoke" "${mca[$i]}"
    done
done
