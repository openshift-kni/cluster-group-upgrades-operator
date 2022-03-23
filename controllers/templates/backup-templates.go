package templates

// Templates for backup job lifecycle

//MngClusterActCreateBackupNS creates namespace
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
        name: backup-agent
`

//MngClusterActCreateSA creates serviceaccount
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

//MngClusterActCreateRB creates clusterrolebinding
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

//MngClusterActCreateBackupJob creates k8s job
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
      spec:
	    activeDeadlineSeconds: {{ .JobTimeout }}
        backoffLimit: 0
        template:
          spec:
            containers:
              -
                args:
                  - launchBackup
                  - "--BackupPath"
                  - /var/recovery
                image: {{ .WorkloadImage }}
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
            serviceAccountname: backup-agent
            volumes:
              -
                hostPath:
                  path: /
                  type: Directory
                name: backup
`

//MngClusterActDeleteNS deletes namespace
const MngClusterActDeleteNS string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec: 
  actionType: Delete
  kube: 
    name: backup-agent
    resource: namespace
`

//MngClusterViewBackupJob creates mcv to monitor k8s job
const MngClusterViewBackupJob string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: jobs
    name: backup-agent
    namespace: openshift-talo-backup
`
