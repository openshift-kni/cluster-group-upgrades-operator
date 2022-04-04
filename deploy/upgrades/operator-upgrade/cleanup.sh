#!/bin/bash

spokes=( "spoke1" "spoke2" "spoke5" )

for spoke in "${spokes[@]}"; do
    echo "$spoke"
    mapfile -t mcv < <(oc get managedclusterview -n "$spoke" | tail -n +2 | awk '{print $1}')
    for (( i=0; i<${#mcv[@]}; i++)); do
        echo "${mcv[$i]}"; oc delete managedclusterview -n "$spoke" "${mcv[$i]}"
    done
    mapfile -t mca < <(oc get managedclusteraction -n "$spoke" | tail -n +2 | awk '{print $1}')
    for (( i=0; i<${#mca[@]}; i++)); do
        oc delete managedclusteraction -n "$spoke" "${mca[$i]}"
    done
done
