#!/bin/bash

cwd=$(dirname "$0")
# shellcheck source=pre-cache/common.sh
. $cwd/common.sh

validate_disk_space() {
    local space_required=${1:-0}
    local rv=1

    APISERVER=https://kubernetes.default.svc
    SERVICEACCOUNT=/var/run/secrets/kubernetes.io/serviceaccount
    TOKEN=$(cat ${SERVICEACCOUNT}/token)
    CACERT=${SERVICEACCOUNT}/ca.crt

    high=85 # default value for imageGCHighThresholdPercent
    kubeletconfig=$(with_retries 3 20 curl --cacert ${CACERT} --header "Authorization: Bearer ${TOKEN}" -X GET ${APISERVER}/api/v1/nodes/${NODE_NAME}/proxy/configz)
    if [ $? -eq 0 ]; then
        val=$(echo $kubeletconfig | chroot /host jq '.kubeletconfig.imageGCHighThresholdPercent')
        if [[ $val -gt 0 && $val -lt 100 ]]; then
            high=$val
        fi
    fi

    size=$(df --output=size /host/var/lib/containers | tail -1)
    used=$(df --output=used /host/var/lib/containers | tail -1)

    log_debug "highThresholdPercent: $high diskSize: $size used: $used"
    log_debug "spaceRequired: ${space_required} GiB"

    if [[ $space_required -gt 0 ]]; then
        rv=$(awk "BEGIN {avail=${high}/100.0*${size}-${used} ; print (avail<${space_required}*1024*1024)}")
    else
        rv=$(awk "BEGIN {avail=${high}/100.0*${size}-${used} ; print (avail<=0)}")
    fi
    [ $rv -ne 0 ] && log_error "not enough space for precaching"
    return $rv
}

if [[ "${BASH_SOURCE[0]}" = "${0}" ]]; then
    validate_disk_space ${1:-0}
    exit $?
fi
