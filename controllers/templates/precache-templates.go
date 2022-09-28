package templates

// Templates for pre-caching lifecycle

// CommonTemplates provides common template metadata
const CommonTemplates string = `
{{ define "actionGVK" }}
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
{{ end }}

{{ define "viewGVK" }}
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
{{ end }}

{{ define "metadata"}}
metadata:
  name: {{ .ResourceName }}
  namespace: {{ .Cluster }}
{{ end }}
`

// MngClusterActCreatePrecachingNS creates namespace
const MngClusterActCreatePrecachingNS string = `
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
        name: openshift-talo-pre-cache
        annotations:
          workload.openshift.io/allowed: management
`

// MngClusterActCreatePrecachingSpecCM creates precachingSpec configmap
const MngClusterActCreatePrecachingSpecCM string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec:
  actionType: Create
  kube:
    resource: configmap
    template:
      apiVersion: v1
      data:
        operators.indexes: |{{ range .Operators.Indexes }}
          {{ . }} {{ end }}
        operators.packagesAndChannels: |{{ range .Operators.PackagesAndChannels }} 
          {{ . }} {{ end }}
        platform.image: {{ .PlatformImage }}
      kind: ConfigMap
      metadata:
        name: pre-cache-spec
        namespace: openshift-talo-pre-cache
`

// MngClusterActCreateServiceAcct creates serviceaccount
const MngClusterActCreateServiceAcct string = `
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
        name: pre-cache-agent
        namespace: openshift-talo-pre-cache
`

// MngClusterActCreateClusterRoleBinding creates clusterrolebinding
const MngClusterActCreateClusterRoleBinding string = `
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
        name: pre-cache-crb
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: cluster-admin
      subjects:
        - kind: ServiceAccount
          name: pre-cache-agent
          namespace: openshift-talo-pre-cache
`

// MngClusterActCreateJob creates precaching k8s job
const MngClusterActCreateJob string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec:
  actionType: Create
  kube:
    resource: job
    namespace: openshift-talo-pre-cache
    template:
      apiVersion: batch/v1
      kind: Job
      metadata:
        name: pre-cache
        namespace: openshift-talo-pre-cache
        annotations:
          target.workload.openshift.io/management: '{"effect":"PreferredDuringScheduling"}'
      spec:
        activeDeadlineSeconds: {{ .JobTimeout }}
        backoffLimit: 0
        template:
          metadata:
            name: pre-cache
            annotations:
              target.workload.openshift.io/management: '{"effect":"PreferredDuringScheduling"}'
          spec:
            containers:
            - args:
              - /opt/precache/precache.sh
              command:
              - /bin/bash
              - -c
              env:
              - name: config_volume_path
                value: /tmp/precache/config
              image: {{ .WorkloadImage }}
              name: pre-cache-container
              resources: {}
              securityContext:
                privileged: true
                runAsUser: 0
              terminationMessagePath: /dev/termination-log
              terminationMessagePolicy: File
              volumeMounts:
              - mountPath: /host
                name: host 
              - mountPath: /etc/config
                name: config-volume
                readOnly: true
            dnsPolicy: ClusterFirst
            restartPolicy: Never
            schedulerName: default-scheduler
            securityContext: {}
            serviceAccountName: pre-cache-agent
            priorityClassName: system-cluster-critical
            volumes:
            - configMap:
                defaultMode: 420
                name: pre-cache-spec
              name: config-volume
            - hostPath:
                path: /
                type: Directory
              name: host

`

// MngClusterViewJob creates mcv to monitor precaching k8s job
const MngClusterViewJob string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: jobs
    name: pre-cache
    namespace: openshift-talo-pre-cache
    updateIntervalSeconds: {{ .ViewUpdateIntervalSec }}
`

// MngClusterViewConfigMap creates mcv to monitor configmap
const MngClusterViewConfigMap string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: configmap
    name: pre-cache-spec
    namespace: openshift-talo-pre-cache
    updateIntervalSeconds: {{ .ViewUpdateIntervalSec }}
`

// MngClusterViewServiceAcct creates mcv to monitor serviceaccount
const MngClusterViewServiceAcct string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: serviceaccounts
    name: pre-cache-agent
    namespace: openshift-talo-pre-cache
    updateIntervalSeconds: {{ .ViewUpdateIntervalSec }}
`

// MngClusterViewClusterRoleBinding creates mcv to monitor clusterrolebinding
const MngClusterViewClusterRoleBinding string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: clusterrolebinding
    name: pre-cache-crb
    updateIntervalSeconds: {{ .ViewUpdateIntervalSec }}
`

// MngClusterViewNamespace creates mcv to monitor namespace
const MngClusterViewNamespace string = `
{{ template "viewGVK"}}
{{ template "metadata" . }}
spec:
  scope:
    resource: namespaces
    name: openshift-talo-pre-cache
    updateIntervalSeconds: {{ .ViewUpdateIntervalSec }}
`

// MngClusterActDeletePrecachingNS deletes prechaching namespace
const MngClusterActDeletePrecachingNS string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec:
  actionType: Delete
  kube:
    resource: namespace
    name: openshift-talo-pre-cache
`
