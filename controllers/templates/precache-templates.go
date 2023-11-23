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
        labels:
          pod-security.kubernetes.io/enforce: privileged
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
        excludePrecachePatterns: |{{ range .ExcludePrecachePatterns }} 
          {{ . }} {{ end }}
        additionalImages: |{{ range .AdditionalImages }}
          {{ . }} {{ end }}
        platform.image: {{ .PlatformImage }}
        spaceRequired: "{{ .SpaceRequired }}"
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
              - name: NODE_NAME
                valueFrom:
                  fieldRef:
                    fieldPath: spec.nodeName
              image: {{ .WorkloadImage }}
              name: pre-cache-container
              resources: {}
              securityContext:
                privileged: true
                runAsUser: 0
              terminationMessagePath: /dev/termination-log
              terminationMessagePolicy: File
              volumeMounts:
                - mountPath: /etc/config
                  name: config-volume
                  readOnly: true
                - mountPath: /host
                  name: host
                - mountPath: /host/etc/containers
                  name: host-etc-containers
                  readOnly: true
                - mountPath: /host/etc/pki/ca-trust
                  name: host-etc-pki-ca-trust
                  readOnly: true
                - mountPath: /host/etc/resolv.conf
                  name: host-etc-resolv-conf
                  readOnly: true
                - mountPath: /host/lib64
                  name: host-lib64
                  readOnly: true
                - mountPath: /host/proc
                  name: host-proc
                  readOnly: true
                - mountPath: /host/run
                  name: host-run
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
                - mountPath: /host/usr/share/containers
                  name: host-usr-share-containers
                  readOnly: true
                - mountPath: /host/var/lib/cni
                  name: host-var-lib-cni
                  readOnly: true
                - mountPath: /host/var/lib/containers
                  name: host-var-lib-containers
                - mountPath: /host/var/lib/kubelet
                  name: host-var-lib-kubelet
                  readOnly: true
                - mountPath: /host/var/tmp
                  name: host-var-tmp
                - mountPath: /host/sys/fs/cgroup
                  name: sys-fs-cgroup
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
              - emptyDir: {}
                name: host
              - hostPath:
                  path: /etc
                  type: Directory
                name: host-etc
              - hostPath:
                  path: /etc/containers
                  type: Directory
                name: host-etc-containers
              - hostPath:
                  path: /etc/pki/ca-trust
                  type: Directory
                name: host-etc-pki-ca-trust
              - hostPath:
                  path: /etc/resolv.conf
                  type: File
                name: host-etc-resolv-conf
              - hostPath:
                  path: /lib64
                  type: Directory
                name: host-lib64
              - hostPath:
                  path: /proc
                  type: Directory
                name: host-proc
              - hostPath:
                  path: /run
                  type: Directory
                name: host-run
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
                  path: /usr/share/containers
                  type: Directory
                name: host-usr-share-containers
              - hostPath:
                  path: /var
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
                  path: /var/lib/kubelet
                  type: Directory
                name: host-var-lib-kubelet
              - hostPath:
                  path: /var/tmp
                  type: Directory
                name: host-var-tmp
              - hostPath:
                  path: /sys/fs/cgroup
                  type: Directory
                name: sys-fs-cgroup
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

// MngClusterActDeletePrecachingCRB deletes prechaching CRB
const MngClusterActDeletePrecachingCRB string = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec:
  actionType: Delete
  kube:
    resource: clusterrolebinding
    name: pre-cache-crb
`
