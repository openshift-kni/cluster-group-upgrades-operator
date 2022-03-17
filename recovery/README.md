# openshift-ai-image-backup

This repository provides a Dockerfile which builds the go binary in the image. The image can be locally build and pushed
to **<https://quay.io/repository/openshift-kni/cluster-group-upgrades-operator-recovery>**.

## Build

To build the golang binary, run **./hack/build-go.sh**

## Kubernetes job

Below kubernetes job can be launched to start the backup procedure from the spoke cluster. To run this task from the
hub, you need a cluster-admin role, or can create a service account and adding the proper security context to it and
pass **serviceAccountName** to the below job.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: container-image
automountServiceAccountToken: false
spec:
  backoffLimit: 0
  template:
    spec:
      containers:
      - name: container-image
        image: quay.io/openshift-kni/cluster-group-upgrades-operator-recovery:latest
        args: ["launchBackup", "--BackupPath", "/var/recovery"]
        securityContext:
          privileged: true
          runAsUser: 0
        tty: true
        volumeMounts:
        - name: container-backup
          mountPath: /host
      hostNetwork: true
      restartPolicy: Never
      volumes:
        - name: container-backup
          hostPath:
            path: /
            type: Directory
```

## Launch the backup from hub with manage cluster action

To launch this job as managed cluster action from the hub, one need to create a namespace, service account,
clusterrolebinding and the job using managed cluster action:

For example:

#### namespace.yaml

```yaml
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
  name: mca-namespace
  namespace: snonode-virt02
spec:
  actionType: Create
  kube:
    resource: namespace
    template:
      apiVersion: v1
      kind: Namespace
      metadata:
        name: backupresource
```

#### serviceAccount.yaml

```yaml
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
  name: mca-serviceaccount
  namespace: snonode-virt02
spec:
  actionType: Create
  kube:
    resource: serviceaccount
    template:
      apiVersion: v1
      kind: ServiceAccount
      metadata:
        name: backupresource
        namespace: backupresource

```

#### clusterrolebinding.yaml

```yaml
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
  name: mca-rolebinding
  namespace: snonode-virt02
spec:
  actionType: Create
  kube:
    resource: clusterrolebinding
    template:
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: backupResource
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: cluster-admin
      subjects:
        - kind: ServiceAccount
          name: backupresource
          namespace: backupresource

```

#### k8sJob.yaml

```yaml
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
  name: mca-ob
  namespace: snonode-virt02
spec:
  actionType: Create
  kube:
    namespace: backupresource
    resource: job
    template:
      apiVersion: batch/v1
      kind: Job
      metadata:
        name: backupresource
      spec:
        backoffLimit: 0
        template:
          spec:
            containers:
              - 
                args:
                  - launchBackup
                  - "--BackupPath"
                  - /var/recovery
                image: quay.io/openshift-kni/cluster-group-upgrades-operator-recovery:latest
                name: container-image
                securityContext:
                  privileged: true
                  runAsUser: 0
                tty: true
                volumeMounts:
                  - 
                    mountPath: /host
                    name: backup
            hostNetwork: true
            restartPolicy: Never
            serviceAccountName: backupresource
            volumes:
              - 
                hostPath:
                  path: /
                  type: Directory
                name: backup
```

## Recovery from Upgrade Failure

### Platform Rollback

Platform rollback, if needed, is handled by issuing the `rpm-ostree rollback -r` command. Not all upgrades will include
an update to the platform OS, however, so this step will not always be required.

When the backup was taken, the active deployment was pinned and standby deployments removed. Use the
`ostree admin status` command to see the current state.

|Deployment Status|Rollback Action|
|-----------------|---------------|
|Single deployment, pinned|Platform OS unchanged, no action required|
|Standby deployment pinned, flagged as "rollback"|Trigger rollback with `rpm-ostree rollback -r` command|
|Multiple standby deployments, pinned deployment is not "rollback"|1. Delete unpinned standby deployments with `ostree admin undeploy 1`, until pinned deployment is "rollback".<br>2. Trigger rollback with `rpm-ostree rollback -r` command|

### Recovery Utility

The upgrade recovery utility is generated when taking the backup, before the upgrade starts, written as `/var/recovery/upgrade-recovery.sh`.

The first phase of the recovery will run the following steps, and then stop to allow the user to reboot the node:

* Shut down `crio.service` and `kubelet.service`, wiping existing containers
* Restore files from the backup to `/etc`, `/usr/local`, and `/var/lib/kubelet`
* Restore any additional machine-config managed files from the backup, if applicable
* Disable `kubelet.service` to prevent it from automatically starting after the reboot

Reboot the node when prompted, with `systemctl reboot`.

The second phase of the recovery can be run after the reboot, with the `--resume` option:<br>
`/var/recovery/upgrade-recovery.sh --resume`

This phase will do the following:

* Restore the etcd cluster, waiting for required containers to restart
* Re-enable kubelet.service, so that it launches automatically on subsequent reboots
* Redeploy etcd, kubeapiserver, kubecontrollermanager, and kubescheduler, waiting for successful redeployment of each,
  per cluster restore procedure documented at:<br>
<https://docs.openshift.com/container-platform/4.9/backup_and_restore/control_plane_backup_and_restore/disaster_recovery/scenario-2-restoring-cluster-state.html>

Should the recovery utility fail, the user can retry with the `--restart` option:<br>
`/var/recovery/upgrade-recovery.sh --restart`
