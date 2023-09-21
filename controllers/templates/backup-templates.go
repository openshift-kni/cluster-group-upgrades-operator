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
                  - mountPath: /host/usr/bin
                    name: host-usr
                    subPath: bin
                    readOnly: true
                  - mountPath: /host/usr/lib
                    name: host-usr
                    subPath: lib
                    readOnly: true
                  - mountPath: /host/usr/lib64
                    name: host-usr
                    subPath: lib64
                    readOnly: true
                  - mountPath: /host/usr/libexec
                    name: host-usr
                    subPath: libexec
                    readOnly: true
                  - mountPath: /host/usr/local
                    name: host-usrlocal
                    readOnly: true
                  - mountPath: /host/proc
                    name: host-proc
                    readOnly: true
                  - mountPath: /host/etc
                    name: host-etc
                    readOnly: true
                  - mountPath: /host/var/recovery
                    name: host-var-recovery
                  - mountPath: /host/var/lib/kubelet
                    name: host-var-lib-kubelet
                    readOnly: true
                  - mountPath: /host/var/lib/etcd
                    name: host-var-lib
                    subPath: etcd
                    readOnly: true
                  - mountPath: /host/var/lib/ovn-ic
                    name: host-var-lib
                    subPath: ovn-ic
                    readOnly: true
                  - mountPath: /host/boot
                    name: host-boot
                    readOnly: true
                  - mountPath: /host/sysroot
                    name: host-sysroot
                  - mountPath: /host/dev/log
                    name: host-dev-log
            restartPolicy: Never
            serviceAccountName: backup-agent
            volumes:
              - emptyDir: {}
                name: host
              - hostPath:
                  path: /usr
                  type: Directory
                name: host-usr
              - hostPath:
                  path: /usr/local
                  type: Directory
                name: host-usrlocal
              - hostPath:
                  path: /etc
                  type: Directory
                name: host-etc
              - hostPath:
                  path: /var/recovery
                  type: Directory
                name: host-var-recovery
              - hostPath:
                  path: /var/lib/
                  type: Directory
                name: host-var-lib
              - hostPath:
                  path: /var/lib/kubelet
                  type: Directory
                name: host-var-lib-kubelet
              - hostPath:
                  path: /proc
                  type: Directory
                name: host-proc
              - hostPath:
                  path: /boot
                  type: Directory
                name: host-boot
              - hostPath:
                  path: /sysroot
                  type: Directory
                name: host-sysroot
              - hostPath:
                  path: /dev/log
                  type: Socket
                name: host-dev-log
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
