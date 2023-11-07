package templates

// Templates for backup job lifecycle

// MngClusterActCreateBackupNS creates namespace
const MngClusterActCreateBackupNS string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec: 
  actionType: Create
  kube: 
    resource: namespace
    template: 
      apiVersion: v1
      kind: Namespace
      metadata: 
        name: openshift-talo-backup
        labels:
          pod-security.kubernetes.io/enforce: privileged
        annotations:
          workload.openshift.io/allowed: management

`

// MngClusterActCreateSA creates serviceaccount
const MngClusterActCreateSA string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec:
  actionType: Create
  kube:
    resource: serviceaccount
    template:
      apiVersion: v1
      kind: ServiceAccount
      metadata:
        name: backup-agent
        namespace: openshift-talo-backup
`

// MngClusterActCreateRB creates clusterrolebinding
const MngClusterActCreateRB string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec:
  actionType: Create
  kube:
    resource: clusterrolebinding
    template:
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: backup-agent
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: cluster-admin
      subjects:
        - kind: ServiceAccount
          name: backup-agent
          namespace: openshift-talo-backup
`

// MngClusterActCreateBackupJob creates k8s job
// Security practices recommend only those directories which are essential to the job should be explicitly mounted.
// Thus, the host filesystem is mounted on a need-to-have basis with most volumes configured to have read-only privilege.
// The sysroot directory and log device are the exception in which these volumes are configured with read-write access.
const MngClusterActCreateBackupJob string = `
{{ template "actionGVK" }}
{{ template "metadata" . }}
spec:
  actionType: Create
  kube:
    namespace: openshift-talo-backup
    resource: job
    template:
      apiVersion: batch/v1
      kind: Job
      metadata:
        name: backup-agent
        namespace: openshift-talo-backup
        annotations:
          target.workload.openshift.io/management: '{"effect":"PreferredDuringScheduling"}'
      spec:
        activeDeadlineSeconds: {{ .JobTimeout }}
        backoffLimit: 0
        template:
          metadata:
            name: backup-agent
            annotations:
              target.workload.openshift.io/management: '{"effect":"PreferredDuringScheduling"}'
          spec:
            containers:
              - args:
                  - launchBackup
                image: {{ .WorkloadImage }} 
                name: container-image
                securityContext:
                  privileged: true
                  runAsUser: 0
                tty: true
                volumeMounts:
                  - mountPath: /host
                    name: host
                  - mountPath: /host/boot
                    name: host-boot
                    readOnly: true
                  - mountPath: /host/dev/log
                    name: host-dev-log
                  - mountPath: /host/etc
                    name: host-etc
                  - mountPath: /host/proc
                    name: host-proc
                    readOnly: true
                  - mountPath: /host/run
                    name: host-run
                  - mountPath: /host/sysroot
                    name: host-sysroot
                  - mountPath: /host/tmp
                    name: host-tmp
                  - mountPath: /host/usr/bin
                    name: host-usr-bin
                    readOnly: true
                  - mountPath: /host/usr/lib
                    name: host-usr-lib
                    readOnly: true
                  - mountPath: /host/usr/lib64
                    name: host-usr-lib64
                    readOnly: true
                  - mountPath: /host/usr/libexec
                    name: host-usr-libexec
                    readOnly: true
                  - mountPath: /host/usr/local
                    name: host-usr-local
                    readOnly: true
                  - mountPath: /host/usr/share/containers
                    name: host-usr-share-containers
                    readOnly: true
                  - mountPath: /host/var/lib/cni
                    name: host-var-lib-cni
                    readOnly: true
                  - mountPath: /host/var/lib/containers
                    name: host-var-lib-containers
                  - mountPath: /host/var/lib/etcd
                    name: host-var-lib-etcd
                    readOnly: true
                  - mountPath: /host/var/lib/kubelet
                    name: host-var-lib-kubelet
                    readOnly: true
                  - mountPath: /host/var/lib/ovn-ic
                    name: host-var-lib-ovn-ic
                    readOnly: true
                  - mountPath: /host/var/recovery
                    name: host-var-recovery
                  - mountPath: /host/var/tmp
                    name: host-var-tmp
                  - mountPath: /host/sys/fs/cgroup
                    name: sys-fs-cgroup
                    readOnly: true
            restartPolicy: Never
            serviceAccountName: backup-agent
            volumes:
              - emptyDir: {}
                name: host
              - hostPath:
                  path: /boot
                  type: Directory
                name: host-boot
              - hostPath:
                  path: /dev/log
                  type: Socket
                name: host-dev-log
              - hostPath:
                  path: /etc
                  type: Directory
                name: host-etc
              - hostPath:
                  path: /proc
                  type: Directory
                name: host-proc
              - hostPath:
                  path: /run
                  type: Directory
                name: host-run
              - hostPath:
                  path: /sysroot
                  type: Directory
                name: host-sysroot
              - hostPath:
                  path: /tmp
                  type: Directory
                name: host-tmp
              - hostPath:
                  path: /usr
                  type: Directory
                name: host-usr
              - hostPath:
                  path: /usr/bin
                  type: Directory
                name: host-usr-bin
              - hostPath:
                  path: /usr/lib
                  type: Directory
                name: host-usr-lib
              - hostPath:
                  path: /usr/lib64
                  type: Directory
                name: host-usr-lib64
              - hostPath:
                  path: /usr/libexec
                  type: Directory
                name: host-usr-libexec
              - hostPath:
                  path: /usr/local
                  type: Directory
                name: host-usr-local
              - hostPath:
                  path: /usr/share-containers
                  type: Directory
                name: host-usr-share-containers
              - hostPath:
                  path: /var/
                  type: Directory
                name: host-var
              - hostPath:
                  path: /var/lib/cni
                  type: Directory
                name: host-var-lib-cni
              - hostPath:
                  path: /var/lib/containers
                  type: Directory
                name: host-var-lib-containers
              - hostPath:
                  path: /var/lib/etcd
                  type: Directory
                name: host-var-lib-etcd
              - hostPath:
                  path: /var/lib/kubelet
                  type: Directory
                name: host-var-lib-kubelet
              - hostPath:
                  path: /var/lib/ovn-ic
                  type: Directory
                name: host-var-lib-ovn-ic
              - hostPath:
                  path: /var/recovery
                  type: DirectoryOrCreate
                name: host-var-recovery
              - hostPath:
                  path: /var/tmp
                  type: Directory
                name: host-var-tmp
              - hostPath:
                  path: /sys/fs/cgroup
                  type: Directory
                name: sys-fs-cgroup
`

// MngClusterActDeleteBackupNS deletes namespace
const MngClusterActDeleteBackupNS string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec: 
  actionType: Delete
  kube: 
    name: openshift-talo-backup
    resource: namespace
`

// MngClusterActDeleteBackupCRB deletes namespace
const MngClusterActDeleteBackupCRB string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec: 
  actionType: Delete
  kube: 
    name: backup-agent
    resource: clusterrolebinding
`

// MngClusterViewBackupJob creates mcv to monitor k8s job
const MngClusterViewBackupJob string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: jobs
    name: backup-agent
    namespace: openshift-talo-backup
`

// MngClusterViewBackupNS creates mcv to monitor spoke cluster's namespace
const MngClusterViewBackupNS string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: namespaces
    name: openshift-talo-backup
`
