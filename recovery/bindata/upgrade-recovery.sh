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
            echo "Retrying after ${delay} seconds, attempt #${attempt}" >&2
            sleep "${delay}"
        fi

        "${cmd[@]}"
        rc=$?
        if [ $rc -eq 0 ]; then
            echo "Command succeeded:" "${cmd[@]}" >&2
            break
        fi

        echo "Command failed:" "${cmd[@]}" >&2
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
    crictl ps 2>/dev/null | awk -v name="${name}" '{if ($(NF-2) == name) {print $1; exit 0}}'
}

function get_container_state {
    local name=$1
    crictl ps 2>/dev/null | awk -v name="${name}" '{if ($(NF-2) == name) {print $(NF-3); exit 0}}'
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

    echo "##### $(date -u): Waiting for ${name} container to restart"

    while [ ${SECONDS} -lt ${timeout} ]; do
        cur_id=$(get_container_id "${name}")
        cur_state=$(get_container_state "${name}")
        if [ -n "${cur_id}" ] && \
                [ "${cur_id}" != "${orig_id}" ] && \
                [ "${cur_state}" = "Running" ]; then
            break
        fi
        echo -n "." && sleep 10
    done

    if [ "$(get_container_state ${name})" != "Running" ]; then
        echo -e "\n$(date -u): ${name} container is not Running. Please investigate" >&2
        exit 1
    fi

    echo -e "\n${name} is running"
    echo "##### $(date -u): ${name} container restarted"
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

    echo "##### $(date -u): Triggering ${name} redeployment"

    starting_rev=$(get_current_revision "${name}")
    starting_latest_rev=$(get_latest_available_revision "${name}")
    if [ -z "${starting_rev}" ] || [ -z "${starting_latest_rev}" ]; then
        echo "Failed to get info for ${name}"
        exit 1
    fi

    expected_rev=$((starting_latest_rev+1))

    echo "Patching ${name}. Starting rev is ${starting_rev}. Expected new rev is ${expected_rev}."
    oc patch "${name}" cluster -p='{"spec": {"forceRedeploymentReason": "recovery-'"$( date --rfc-3339=ns )"'"}}' --type=merge
    if [ $? -ne 0 ]; then
        echo "Failed to patch ${name}. Please investigate" >&2
        exit 1
    fi

    while [ $SECONDS -lt $timeout ]; do
        cur_rev=$(get_current_revision "${name}")
        if [ -z "${cur_rev}" ]; then
            echo -n "."; sleep 10
            continue # intermittent API failure
        fi

        if [[ ${cur_rev} -ge ${expected_rev} ]]; then
            echo -e "\n${name} redeployed successfully: revision ${cur_rev}"
            break
        fi
        echo -n "."; sleep 10
    done

    cur_rev=$(get_current_revision "${name}")
    if [[ ${cur_rev} -lt ${expected_rev} ]]; then
        echo "Failed to redeploy ${name}. Please investigate" >&2
        exit 1
    fi

    echo "##### $(date -u): Completed ${name} redeployment"
}

#
# take_backup:
# Procedure for backing up data prior to upgrade
#
function take_backup {
    echo "##### $(date -u): Taking backup"

    echo "##### $(date -u): Wiping previous deployments and pinning active"
    while :; do
        ostree admin undeploy 1 || break
    done
    ostree admin pin 0
    if [ $? -ne 0 ]; then
        echo "Failed to pin active deployment" >&2
        exit 1
    fi

    echo "##### $(date -u): Backing up container cluster and required files"

    /usr/local/bin/cluster-backup.sh ${BACKUP_DIR}/cluster
    if [ $? -ne 0 ]; then
        echo "Cluster backup failed" >&2
        exit 1
    fi

    cat /etc/tmpfiles.d/* | sed 's/#.*//' | awk '{print $2}' | grep '^/etc/' | sed 's#^/etc/##' > ${BACKUP_DIR}/etc.exclude.list
    echo '.updated' >> ${BACKUP_DIR}/etc.exclude.list
    echo 'kubernetes/manifests' >> ${BACKUP_DIR}/etc.exclude.list
    with_retries 3 1 rsync -a /etc/ ${BACKUP_DIR}/etc/
    if [ $? -ne 0 ]; then
        echo "Failed to backup /etc" >&2
        exit 1
    fi

    with_retries 3 1 rsync -a /usr/local/ ${BACKUP_DIR}/usrlocal/
    if [ $? -ne 0 ]; then
        echo "Failed to backup /usr/local" >&2
        exit 1
    fi

    with_retries 3 1 rsync -a /var/lib/kubelet/ ${BACKUP_DIR}/kubelet/
    if [ $? -ne 0 ]; then
        echo "Failed to backup /var/lib/kubelet" >&2
        exit 1
    fi

    oc get mc -o=jsonpath='{range .items[*]}{range .spec.config.storage.files[*]}{.path}{"\n"}' | sort -u \
        | grep -v -e '^/etc/' -e '^/usr/local/' -e '/var/lib/kubelet/' -e '^$' \
        | xargs --no-run-if-empty tar czf ${BACKUP_DIR}/extras.tgz
    if [ $? -ne 0 ]; then
        echo "Failed to backup additional managed files" >&2
        exit 1
    fi

    echo "##### $(date -u): Backup complete"
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

    #
    # Wipe current containers by shutting down kubelet, deleting containers and pods,
    # then stopping and wiping crio
    #
    echo "##### $(date -u): Wiping existing containers"
    systemctl stop kubelet.service
    crictl rmp -fa
    systemctl stop crio.service
    crio wipe -f
    echo "##### $(date -u): Completed wipe"

    #
    # Restore /usr/local content
    #
    echo "##### $(date -u): Restoring /usr/local content"
    time with_retries 3 1 rsync -avc --delete --no-t ${BACKUP_DIR}/usrlocal/ /usr/local/
    if [ $? -ne 0 ]; then
        echo "$(date -u): Failed to restore /usr/local content" >&2
        exit 1
    fi
    echo "##### $(date -u): Completed restoring /usr/local content"

    #
    # Restore /var/lib/kubelet content
    #
    echo "##### $(date -u): Restoring /var/lib/kubelet content"
    time with_retries 3 1 rsync -avc --delete --no-t ${BACKUP_DIR}/kubelet/ /var/lib/kubelet/
    if [ $? -ne 0 ]; then
        echo "$(date -u): Failed to restore /var/lib/kubelet content" >&2
        exit 1
    fi
    echo "##### $(date -u): Completed restoring /var/lib/kubelet content"

    #
    # Restore /etc content
    #
    echo "##### $(date -u): Restoring /etc content"
    time with_retries 3 1 rsync -avc --delete --no-t --exclude-from ${BACKUP_DIR}/etc.exclude.list ${BACKUP_DIR}/etc/ /etc/
    if [ $? -ne 0 ]; then
        echo "$(date -u): Failed to restore /etc content" >&2
        exit 1
    fi
    echo "##### $(date -u): Completed restoring /etc content"

    #
    # Restore additional machine-config managed files
    #
    if [ -f ${BACKUP_DIR}/extras.tgz ]; then
        echo "##### $(date -u): Restoring extra content"
        tar xzf ${BACKUP_DIR}/extras.tgz -C /
        if [ $? -ne 0 ]; then
            echo "$(date -u): Failed to restore extra content" >&2
            exit 1
        fi
        echo "##### $(date -u): Completed restoring extra content"
    fi

    #
    # As systemd files may have been updated as part of the preceding restores,
    # run daemon-reload
    #
    systemctl daemon-reload
    systemctl disable kubelet.service

    record_progress "restore_files"

    echo "Please reboot now with 'systemctl reboot', then run '${PROG} --resume'" >&2
    exit 0
}

function restore_cluster {
    #
    # Start crio, if needed
    #
    if ! systemctl -q is-active crio.service; then
        echo "##### $(date -u): Starting crio.service"
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
    echo "##### $(date -u): Restoring cluster"
    time /usr/local/bin/cluster-restore.sh ${BACKUP_DIR}/cluster
    if [ $? -ne 0 ]; then
        echo "$(date -u): Failed to restore cluster" >&2
        exit 1
    fi

    echo "##### $(date -u): Restarting kubelet.service"
    time systemctl restart kubelet.service
    systemctl enable kubelet.service

    echo "##### $(date -u): Restarting crio.service"
    time systemctl restart crio.service

    #
    # Wait for containers to launch or restart after cluster restore
    #
    echo "##### $(date -u): Waiting for required container restarts"

    time wait_for_container_restart etcd "${ORIG_ETCD_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart etcd-operator "${ORIG_ETCD_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart kube-apiserver-operator "${ORIG_KUBE_APISERVER_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart kube-controller-manager-operator "${ORIG_KUBE_CONTROLLER_MANAGER_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}
    time wait_for_container_restart kube-scheduler-operator-container "${ORIG_KUBE_SCHEDULER_OPERATOR_CONTAINER_ID}" ${RESTART_TIMEOUT}

    echo "##### $(date -u): Required containers have restarted"

    record_progress "restore_cluster"
}

function post_restore_steps {
    #
    # Trigger required resource redeployments
    #
    echo "##### $(date -u): Triggering redeployments"

    time trigger_redeployment etcd ${REDEPLOYMENT_TIMEOUT}
    time trigger_redeployment kubeapiserver ${REDEPLOYMENT_TIMEOUT}
    time trigger_redeployment kubecontrollermanager ${REDEPLOYMENT_TIMEOUT}
    time trigger_redeployment kubescheduler ${REDEPLOYMENT_TIMEOUT}

    echo "##### $(date -u): Redeployments complete"

    echo "##### $(date -u): Recovery complete"

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
    usage
    exit 1
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
            usage
            exit 1
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
        [ ! -d "${BACKUP_DIR}/usrlocal" ] || \
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

echo "##### $(date -u): Recovery complete"

clear_progress

