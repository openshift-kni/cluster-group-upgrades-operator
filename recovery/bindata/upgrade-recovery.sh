#!/bin/bash
#
# This script manages post-rollback recovery.
# This utility runs after the platform has been rolled back with "rpm-ostree rollback -r", if needed.
#

#
# References:
#
# Cluster recovery procedure is based on the following:
# https://docs.openshift.com/container-platform/4.9/backup_and_restore/control_plane_backup_and_restore/disaster_recovery/scenario-2-restoring-cluster-state.html
#
# CRI-O wipe procedure is based on the following:
# https://docs.openshift.com/container-platform/4.9/support/troubleshooting/troubleshooting-crio-issues.html#cleaning-crio-storage
#

declare PROG=
PROG=$(basename "$0")

function usage {
    cat <<ENDUSAGE
${PROG}: Runs post-rollback restore procedure

Options:
    --dir <dir>:    Location of backup content

Backup options:
    --take-backup:  Take backup

Recovery options:
    --force:        Skip ostree deployment check
    --step:         Step through recovery stages
    --resume:       Resume recovery after last successful stage
    --restart:      Restart recovery from first stage
ENDUSAGE
    exit 1
}

function log {
    local level=$1
    shift

    logger -t ${PROG} --id=$$ "${level}: $*"
}

function log_info {
    log "INFO" "$*"
    echo "##### $(date -u): $*"
}

function log_error {
    log "ERROR" "$*"
    echo "##### $(date -u): $*" >&2
}

function log_debug {
    echo "$*"
}
function fatal {
    log_error "$*"
    exit 1
}

#
# with_retries:
# Helper function to run a command with retries on failure
#
function with_retries {
    local max_attempts=$1
    local delay=$2
    local cmd=("${@:3}")
    local attempt=0
    local rc=0

    while [[ ${attempt} -lt ${max_attempts} ]]; do
        attempt=$((attempt+=1))
        if [[ ${attempt} -gt 1 ]]; then
            log_debug "Retrying after ${delay} seconds, attempt #${attempt}"
            sleep "${delay}"
        fi

        "${cmd[@]}"
        rc=$?
        if [ $rc -eq 0 ]; then
            log_debug "Command succeeded:" "${cmd[@]}"
            break
        fi

        log_error "Command failed:" "${cmd[@]}"
    done

    return ${rc}
}

#
# display_current_status:
# For informational purposes only
#
function display_current_status {
    echo "##### $(date -u): Displaying current status"

    echo "##### $(date -u): oc adm upgrade && oc get co && oc get nodes -o wide && oc get mcp"
    oc adm upgrade && oc get co && oc get nodes -o wide && oc get mcp
    echo "##### $(date -u): Done"
}

function get_container_id {
    local name=$1
    crictl ps -o json 2>/dev/null | jq -r --arg name "${name}" '.containers[] | select(.metadata.name==$name).id'
}

function get_container_state {
    local name=$1
    crictl ps -o json 2>/dev/null | jq -r --arg name "${name}" '.containers[] | select(.metadata.name==$name).state'
}

function get_current_revision {
    local name=$1
    oc get "${name}" -o=jsonpath='{.items[0].status.nodeStatuses[0].currentRevision}{"\n"}' 2>/dev/null
}

function get_latest_available_revision {
    local name=$1
    oc get "${name}" -o=jsonpath='{.items[0].status.latestAvailableRevision}{"\n"}' 2>/dev/null
}

#
# wait_for_container_restart:
# Polls container status, waiting until the specified container has been
# launched or restarted and in a Running state
#
function wait_for_container_restart {
    local name=$1
    local orig_id=$2
    local timeout=
    timeout=$((SECONDS+$3))

    local cur_id=
    local cur_state=

    log_info "Waiting for ${name} container to restart"

    while [ ${SECONDS} -lt ${timeout} ]; do
        cur_id=$(get_container_id "${name}")
        cur_state=$(get_container_state "${name}")
        if [ -n "${cur_id}" ] && \
                [ "${cur_id}" != "${orig_id}" ] && \
                [ "${cur_state}" = "CONTAINER_RUNNING" ]; then
            break
        fi
        echo -n "." && sleep 10
    done
    echo

    if [ "$(get_container_state ${name})" != "CONTAINER_RUNNING" ]; then
        fatal "${name} container is not Running. Please investigate"
    fi

    log_info "${name} container restarted"
}

#
# trigger_redeployment:
# Patches a given resource to trigger a new revision, and polls until
# redeployment is complete
#
function trigger_redeployment {
    local name=$1
    local timeout=
    timeout=$((SECONDS+$2))

    local starting_rev=
    local starting_latest_rev=
    local cur_rev=
    local expected_rev=

    log_info "Triggering ${name} redeployment"

    starting_rev=$(get_current_revision "${name}")
    starting_latest_rev=$(get_latest_available_revision "${name}")
    if [ -z "${starting_rev}" ] || [ -z "${starting_latest_rev}" ]; then
        fatal "Failed to get info for ${name}"
    fi

    expected_rev=$((starting_latest_rev+1))

    log_debug "Patching ${name}. Starting rev is ${starting_rev}. Expected new rev is ${expected_rev}."
    oc patch "${name}" cluster -p='{"spec": {"forceRedeploymentReason": "recovery-'"$( date --rfc-3339=ns )"'"}}' --type=merge
    if [ $? -ne 0 ]; then
        fatal "Failed to patch ${name}. Please investigate"
    fi

    while [ $SECONDS -lt $timeout ]; do
        cur_rev=$(get_current_revision "${name}")
        if [ -z "${cur_rev}" ]; then
            echo -n "."; sleep 10
            continue # intermittent API failure
        fi

        if [[ ${cur_rev} -ge ${expected_rev} ]]; then
            echo
            log_debug "${name} redeployed successfully: revision ${cur_rev}"
            break
        fi
        echo -n "."; sleep 10
    done

    cur_rev=$(get_current_revision "${name}")
    if [[ ${cur_rev} -lt ${expected_rev} ]]; then
        fatal "Failed to redeploy ${name}. Please investigate"
    fi

    log_info "Completed ${name} redeployment"
}

#
# take_backup:
# Procedure for backing up data prior to upgrade
#
function take_backup {
    log_info "Taking backup"

    log_info "Wiping previous deployments and pinning active"
    while :; do
        ostree admin undeploy 1 || break
    done
    ostree admin pin 0
    if [ $? -ne 0 ]; then
        fatal "Failed to pin active deployment"
    fi

    log_info "Backing up container cluster and required files"

    # WARNING:
    # passing a --force option will skip checks for the state of
    # operators: kube-apiserver, kube-scheduler, etcd, kube-controller-manager
    # As a result the "__POSSIBLY_DIRTY__" suffix will be added to the saved db name
    # which is required when the pod can't reach to k8s api server due to 
    # the hostnetwork being false in the pod.
    /usr/local/bin/cluster-backup.sh --force ${BACKUP_DIR}/cluster
    if [ $? -ne 0 ]; then
        fatal "Cluster backup failed"
    fi

    cat /etc/tmpfiles.d/* | sed 's/#.*//' | awk '{print $2}' | grep '^/etc/' | sed 's#^/etc/##' > ${BACKUP_DIR}/etc.exclude.list
    echo '.updated' >> ${BACKUP_DIR}/etc.exclude.list
    echo 'kubernetes/manifests' >> ${BACKUP_DIR}/etc.exclude.list
    with_retries 3 1 cp -Ra /etc/ ${BACKUP_DIR}/
    if [ $? -ne 0 ]; then
        fatal "Failed to backup /etc"
    fi

    with_retries 3 1 cp -Ra /usr/local/ ${BACKUP_DIR}/
    if [ $? -ne 0 ]; then
        fatal "Failed to backup /usr/local"
    fi

    with_retries 3 1 cp -Ra /var/lib/kubelet/ ${BACKUP_DIR}/
    if [ $? -ne 0 ]; then
        fatal "Failed to backup /var/lib/kubelet"
    fi

    jq -r '.spec.config.storage.files[].path' < /etc/machine-config-daemon/currentconfig  \
        | grep -v -e '^/etc/' -e '^/usr/local/' -e '/var/lib/kubelet/' -e '^$' \
        | xargs --no-run-if-empty tar czf ${BACKUP_DIR}/extras.tgz
    if [ $? -ne 0 ]; then
        fatal "Failed to backup additional managed files"
    fi

    log_info "Backup complete"
}

function is_restore_in_progress {
    test -f "${PROGRESS_FILE}"
}

function record_progress {
    grep -q "^$1$" "${PROGRESS_FILE}" 2>/dev/null || echo "$1" >> "${PROGRESS_FILE}"
}

function check_progress {
    grep -q "^$1$" "${PROGRESS_FILE}" 2>/dev/null
}

function clear_progress {
    rm -f "${PROGRESS_FILE}"
}

function check_active_deployment {
    #
    # If the current deployment is not pinned, assume the platform has not been rolled back
    #
    if ! ostree admin status | grep -A 3 '^\*' | grep -q 'Pinned: yes'; then
        if [ "${SKIP_DEPLOY_CHECK}" = "yes" ]; then
            echo "Warning: Active ostree deployment is not pinned and should be rolled back."
        else
            echo "Active ostree deployment is not pinned and should be rolled back." >&2
            exit 1
        fi
    fi
}

function restore_files {
    display_current_status

    setenforce 0
    if [ $? -ne 0 ]; then
        fatal "Failed to enter permissive mode"
    fi

    #
    # Wipe current containers by shutting down kubelet, deleting containers and pods,
    # then stopping and wiping crio
    #
    log_info "Wiping existing containers"
    systemctl stop kubelet.service
    crictl rmp -fa
    systemctl stop crio.service
    crio wipe -f
    log_info "Completed wipe"

    #
    # Restore /usr/local content
    #
    log_info "Restoring /usr/local content"
    time with_retries 3 1 rsync -aAXvc --delete --no-t ${BACKUP_DIR}/local/ /usr/local/
    if [ $? -ne 0 ]; then
        fatal "Failed to restore /usr/local content"
    fi
    log_info "Completed restoring /usr/local content"

    #
    # Restore /etc content
    #
    log_info "Restoring /etc content"
    time with_retries 3 1 rsync -aAXvc --delete --no-t --exclude-from ${BACKUP_DIR}/etc.exclude.list ${BACKUP_DIR}/etc/ /etc/
    if [ $? -ne 0 ]; then
        fatal "Failed to restore /etc content"
    fi
    log_info "Completed restoring /etc content"

    #
    # Restore additional machine-config managed files
    #
    if [ -f ${BACKUP_DIR}/extras.tgz ]; then
        log_info "Restoring extra content"
        tar xzf ${BACKUP_DIR}/extras.tgz -C /
        if [ $? -ne 0 ]; then
            fatal "Failed to restore extra content"
        fi
        log_info "Completed restoring extra content"
    fi

    #
    # As systemd files may have been updated as part of the preceding restores,
    # run daemon-reload
    #
    systemctl daemon-reload
    systemctl disable kubelet.service

    record_progress "restore_files"

    setenforce 1

    echo "Please reboot now with 'systemctl reboot', then run '${PROG} --resume'" >&2
    exit 0
}

function restore_cluster {
    setenforce 0
    if [ $? -ne 0 ]; then
        fatal "Failed to enter permissive mode"
    fi

    #
    # Restore /var/lib/kubelet content
    #
    log_info "Restoring /var/lib/kubelet content"
    time with_retries 3 1 rsync -aAXvc --delete --no-t ${BACKUP_DIR}/kubelet/ /var/lib/kubelet/
    if [ $? -ne 0 ]; then
        fatal "Failed to restore /var/lib/kubelet content"
    fi
    log_info "Completed restoring /var/lib/kubelet content"

    #
    # Start crio, if needed
    #
    if ! systemctl -q is-active crio.service; then
        log_info "Starting crio.service"
        systemctl start crio.service
    fi

    #
    # Get current container IDs
    #
    ORIG_ETCD_CONTAINER_ID=$(get_container_id etcd)
    ORIG_ETCD_OPERATOR_CONTAINER_ID=$(get_container_id etcd-operator)
    ORIG_KUBE_APISERVER_OPERATOR_CONTAINER_ID=$(get_container_id kube-apiserver-operator)
    ORIG_KUBE_CONTROLLER_MANAGER_OPERATOR_CONTAINER_ID=$(get_container_id kube-controller-manager-operator)
    ORIG_KUBE_SCHEDULER_OPERATOR_CONTAINER_ID=$(get_container_id kube-scheduler-operator-container)

    #
    # Restore cluster
    #
    log_info "Restoring cluster"
    time /usr/local/bin/cluster-restore.sh ${BACKUP_DIR}/cluster
    if [ $? -ne 0 ]; then
        fatal "Failed to restore cluster"
    fi

    log_info "Restarting kubelet.service"
    time systemctl restart kubelet.service
    systemctl enable kubelet.service

    log_info "Restarting crio.service"
    time systemctl restart crio.service

    #
    # Wait for containers to launch or restart after cluster restore
    #
    log_info "Waiting for required container restarts"

    time wait_for_container_restart etcd "${ORIG_ETCD_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart etcd-operator "${ORIG_ETCD_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart kube-apiserver-operator "${ORIG_KUBE_APISERVER_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart kube-controller-manager-operator "${ORIG_KUBE_CONTROLLER_MANAGER_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart kube-scheduler-operator-container "${ORIG_KUBE_SCHEDULER_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}

    log_info "Required containers have restarted"

    record_progress "restore_cluster"

    setenforce 1
}

function post_restore_steps {
    #
    # Trigger required resource redeployments
    #
    log_info "Triggering redeployments"

    time trigger_redeployment etcd ${REDEPLOYMENT_TIMEOUT}
    time trigger_redeployment kubeapiserver ${REDEPLOYMENT_TIMEOUT}
    time trigger_redeployment kubecontrollermanager ${REDEPLOYMENT_TIMEOUT}
    time trigger_redeployment kubescheduler ${REDEPLOYMENT_TIMEOUT}

    log_info "Redeployments complete"

    display_current_status
}

#
# Process command-line arguments
#
declare BACKUP_DIR="/var/recovery"
declare RESTART_TIMEOUT=1200 # 20 minutes
declare REDEPLOYMENT_TIMEOUT=1200 # 20 minutes
declare SKIP_DEPLOY_CHECK="no"
declare TAKE_BACKUP="no"
declare STEPTHROUGH="no"
declare RESUME="no"

LONGOPTS="dir:,force,restart,resume,step,take-backup"
OPTS=$(getopt -o h --long "${LONGOPTS}" --name "$0" -- "$@")

if [ $? -ne 0 ]; then
    # usage will exit automatically
    usage
fi

eval set -- "${OPTS}"

while :; do
    case "$1" in
        --dir)
            BACKUP_DIR=$2
            shift 2
            ;;
        --force)
            SKIP_DEPLOY_CHECK="yes"
            shift
            ;;
        --restart)
            STEPTHROUGH_RESET="yes"
            shift
            ;;
        --resume)
            RESUME="yes"
            shift
            ;;
        --step)
            STEPTHROUGH="yes"
            shift
            ;;
        --take-backup)
            TAKE_BACKUP="yes"
            shift
            ;;
        --)
            shift
            break
            ;;
        *)
            # usage will exit automatically
            usage
            ;;
    esac
done

declare PROGRESS_FILE="${BACKUP_DIR}/progress"

# shellcheck source=/dev/null
source /etc/kubernetes/static-pod-resources/etcd-certs/configmaps/etcd-scripts/etcd-common-tools

#
# Perform backup and exit, if requested
#
if [ "${TAKE_BACKUP}" = "yes" ]; then
    take_backup
    exit 0
fi

#
# Validate environment
#
if [ -z "${KUBECONFIG}" ] || [ ! -r "${KUBECONFIG}" ]; then
    echo "Please provide kubeconfig location in KUBECONFIG env variable" >&2
    exit 1
fi

#
# Validate arguments
#
if [ ! -d "${BACKUP_DIR}/cluster" ] || \
        [ ! -d "${BACKUP_DIR}/etc" ] || \
        [ ! -d "${BACKUP_DIR}/local" ] || \
        [ ! -d "${BACKUP_DIR}/kubelet" ]; then
    echo "Required backup content not found in ${BACKUP_DIR}" >&2
    exit 1
fi

#
# Clear progress flag, if requested
#
if [ "${STEPTHROUGH_RESET}" = "yes" ]; then
    clear_progress
fi

#
# Check whether a restore has already started
#
if [ "${RESUME}" = "no" ] && [ "${STEPTHROUGH}" = "no" ] && is_restore_in_progress; then
    echo "Restore has already started. Use --restart option to restart, or --step to resume" >&1
    exit 1
fi

if ! is_restore_in_progress; then
    check_active_deployment
fi

record_progress "started"

if ! check_progress "restore_files"; then
    restore_files

    # shellcheck disable=SC2317
    if [ "${STEPTHROUGH}" = "yes" ]; then
        echo "##### $(date -u): Stage complete. Use --step option to resume."
        exit 0
    fi
fi

if ! check_progress "restore_cluster"; then
    restore_cluster

    if [ "${STEPTHROUGH}" = "yes" ]; then
        echo "##### $(date -u): Stage complete. Use --step option to resume."
        exit 0
    fi
fi

post_restore_steps

log_info "Recovery complete"

clear_progress

exit 0
