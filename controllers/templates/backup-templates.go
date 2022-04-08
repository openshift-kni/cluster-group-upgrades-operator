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

// TODO: image must use var {{ .WorkloadImage }} rather than hard coded image
// during template initialization, this var must be recovered from csv

// MngClusterActCreateBackupJob creates k8s job
const MngClusterActCreateBackupJob string = `
{{ template "actionGVK"}}
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
            restartPolicy: Never
            hostNetwork: true
            serviceAccountName: backup-agent
            volumes:
              -
                hostPath:
                  path: /
                  type: Directory
                name: backup
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
