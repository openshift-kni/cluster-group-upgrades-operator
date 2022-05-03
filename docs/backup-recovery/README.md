# Cluster upgrades - backup and recovery #

## Background ##

This feature creates a pre-upgrade backup and provides a procedure for rapid recovery of a SNO in the event of a failed upgrade. In case of an upgrade failure, this feature allows the SNO to be restored to a working state with the previous version of OCP without requiring a re-provision of the application(s).  

## Create Recovery Partition ##

> **WARNING**: It is highly recommended to create a recovery partition at install time if opted to use this feature.

There are two methods related to partition creation is described here:

### Preferred method with Siteconfig ###

The recovery partition can be created at install time by defining a node level property called `diskPartition` in the SiteConfig. The configuration for the partition must be defined by `start` parameter which must indicate the end of the root device. Additionally, a spare disk can be used for recovery partition as well. A recovery partition of 50GB can be created by following the below example:

```
nodes:
      - hostName: "snonode.sno-worker-0.e2e.bos.redhat.com"
        role: "master"
        rootDeviceHints:
          hctl: "0:2:0:0"
          deviceName: /dev/sda
        ........
        ........
        #Disk /dev/sda: 893.3 GiB, 959119884288 bytes, 1873281024 sectors
        diskPartition:
          - device: /dev/sda
            partitions:
              - mount_point: /var/recovery
                size: 51200
                start: 800000

```

### Alternative method with Extra-manifest

The alternative way a recovery partition can be created at install time by defining an extra-manifest MachineConfig, as described here:<br>
<https://github.com/openshift-kni/cnf-features-deploy/blob/master/ztp/gitops-subscriptions/argocd/README.md#deploying-a-site>

The following extra-manifest MachineConfig will create a 50G partition on the specified disk:

```
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
          wipeTable: true # Wipe the disk
          partitions:
          - label: recovery
            startMiB: 1 # Optional
            sizeMiB: 51200
      filesystems:
        - device: /dev/disk/by-partlabel/recovery
          path: /var/recovery
          format: xfs
          wipe_filesystem: true
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
- TALO checks the backup requirement in the TALO CR. If required, deploys a backup workload on the designated spoke.  

### States ###

- BackupStatePreparingToStart - is the initial state all clusters are automatically assigned to on the first reconciliation pass of the TALO CR. Upon entry TALO deletes spoke backup namespace and hub view resources that might have remained from the prior incomplete attempts.
- BackupStateStarting - is the state for creation of the backup job pre-requisites and the job itself
- BackupStateActive - the job is in "Active" state
- BackupStateSucceeded - a final state reached when the backup job has succeeded
- BackupStateTimeout - a final state meaning that artifact backup has been partially done
- BackupStateError - a final state reached when the job ends with a non-zero exit code

### On the spoke ###

The backup workload generates an utility called `upgrade-recovery.sh` in the recovery partition or at the recovery folder at `/var/recovery` and takes the pre-upgrade backup. In addition, the active OS deployment is pinned using ostree and the standby deployments are removed.

#### Procedure end options ####

- Success (“Completed”)
- Failure due to timeout (“DeadlineExceeded”)  
  - the procedure did not complete due to a timeout and can be continued
  - Unrecoverable error (“BackoffLimitExceeded”) - user intervention is desired to determine the failure reason


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

* The first phase of the recovery shuts down containers and cleans up, and restore files from the backup partition/folder to the targeted directories. Afterwards, it prompts for a rebooting the node with `systemctl reboot`.

* The second stage restores cluster database and related files from the backup, relaunches containers and trigger required redeployments as per cluster restore procedure.


> **WARNING**: Should the recovery utility fail, the user can retry with the `--restart` option:<br>
`/var/recovery/upgrade-recovery.sh --restart`



