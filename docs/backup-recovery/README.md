# Cluster upgrades - backup and recovery #

## Background ##

This feature creates a pre-upgrade backup and provides a procedure for rapid recovery of a SNO in the event of a failed upgrade. In case of an upgrade failure, this feature allows the SNO to be restored to a working state with the previous version of OCP without requiring a re-provision of the application(s).  

## Create Recovery Partition ##

> **WARNING**: It is highly recommended to create a recovery partition at install time if opted to use this feature.

There are two methods related to partition creation is described here:

### Preferred method with Siteconfig ###

The recovery partition can be created at install time by passing disk and filesystem information at the node level property called `ignitionConfigOverride` in the SiteConfig CR.
It requires to define ignition version, disks and filesystems information. The device under disks must be referenced by a persistent path, such as those under /dev/disk/by-path or /dev/disk/by-id (where the by-path name would be recommended, as it is not hardware-specific and would not change in the case of disk replacement)


Additionally, a spare disk can be used for recovery partition as well and can be configured at provisioning time or at day 2 operation. A recovery partition of 50GB with mountpoint at `/var/recovery` can be created by following the below example. 

```yaml
apiVersion: ran.openshift.io/v1
kind: SiteConfig
........
nodes:
  - hostName: "snonode.sno-worker-0.e2e.bos.redhat.com"
    role: "master"
    ignitionConfigOverride: '{"ignition":{"version":"3.2.0"},"storage":{"disks":[{"device":"/dev/disk/by-path/pci-0000:18:00.0-scsi-0:2:1:0","wipeTable":false,"partitions":[{"sizeMiB":51200,"label":"recovery","startMiB":800000, "wipePartitionEntry": true}]}],"filesystem":[{"device":"/dev/disk/by-partlabel/recovery","path":"/var/recovery","format":"xfs","wipeFilesystem":true}]}}'
```
In the configuration for the partitions, `startMiB` parameter indicates, in mebibytes, the end of the `/sysroot` RHCOS partition and the start for the newly requested recovery partition. This is an optional parameter.

### Alternative method with Extra-manifest

As an alternative way, a recovery partition can also be created at install time by defining an extra-manifest MachineConfig, as described here:<br>
<https://github.com/openshift-kni/cnf-features-deploy/blob/master/ztp/gitops-subscriptions/argocd/README.md#deploying-a-site>

The following extra-manifest MachineConfig example will create a 50G partition on the specified secondary disk:
<br>(NOTE: If using root disk for recovery partition, do not set `wipeTable: true`)

```yaml
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 98-var-recovery-partition
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      disks:
        # Use persistent disk path for device, as /dev/sd* names can change
        - device: /dev/disk/by-path/pci-0000:18:00.0-scsi-0:2:1:0
          wipeTable: false # if true, will wipe the disk. WARNING: Do not set true if using root disk
          partitions:
          - label: recovery
            startMiB: 1 # Optional
            sizeMiB: 51200
            wipePartitionEntry: true
      filesystems:
        - device: /dev/disk/by-partlabel/recovery
          path: /var/recovery
          format: xfs
          wipeFilesystem: true
    systemd:
      units:
        - name: var-recovery.mount
          enabled: true
          contents: |
            [Unit]
            Before=local-fs.target
            [Mount]
            What=/dev/disk/by-partlabel/recovery
            Where=/var/recovery
            Options=defaults
            [Install]
            WantedBy=local-fs.target
```

## Backup workload and the procedure ##

Backup workload is a one-shot task created on each of the spoke cluster nodes to trigger backup and keep the backup and system resources in the recovery partition in order to recovery a failed upgrade. For SNO spokes it is realized as a `batch/v1 job`.


### On the hub ###

- User creates a TALO CR that defines:
  - A set of clusters to be upgraded  
  - The need for resource backup
- User applies the TALO CR to the hub cluster at the beginning of the maintenance window
- TALO checks the backup requirement in the TALO CR. If required, deploys a backup workload on the designated spoke when enable field is set to true.  

### States ###

- BackupStatePreparingToStart - is the initial state all clusters are automatically assigned to on the first reconciliation pass of the TALO CR. Upon entry TALO deletes spoke backup namespace and hub view resources that might have remained from the prior incomplete attempts.
- BackupStateStarting - is the state for creation of the backup job pre-requisites and the job itself
- BackupStateActive - the job is in "Active" state
- BackupStateSucceeded - a final state reached when the backup job has succeeded
- BackupStateTimeout - a final state meaning that artifact backup has been partially done
- BackupStateError - a final state reached when the job ends with a non-zero exit code

> **WARNING**: Should the backup job fails and enters to `BackupStateTimeout` or `BackupStateError` state, it will block the cluster upgrade.

### On the spoke ###

The backup workload generates an utility called `upgrade-recovery.sh` in the recovery partition or at the recovery folder at `/var/recovery` and takes the pre-upgrade backup. In addition, the active OS deployment is pinned using ostree and the standby deployments are removed.

#### Procedure end options ####

- Success (“Completed”)
- Failure due to timeout (“DeadlineExceeded”)  
  - the procedure did not complete due to a timeout and can be continued
  - Unrecoverable error (“BackoffLimitExceeded”) - user intervention is desired to determine the failure reason


## Recovery from Upgrade Failure

In case, the upgrade failed in a spoke cluster, the TALO CR needs to be deleted in the hub cluster and an admin needs to login to the spoke cluster to start the recovery process.

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

* To start the recovery, you have to launch the script. The first phase of the recovery shuts down containers and cleans up, and restore files from the backup partition/folder to the targeted directories. Afterwards, it prompts for a rebooting the node with `systemctl reboot`.

* After the reboot, the script needs to be relaunched again with the resume option, i.e. `/var/recovery/upgrade-recovery.sh --resume`. This will restore cluster database and related files from the backup, will relaunch containers and trigger required redeployments as per cluster restore procedure.


> **WARNING**: Should the recovery utility fail, the user can retry with the `--restart` option:<br>
`/var/recovery/upgrade-recovery.sh --restart`



