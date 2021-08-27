/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

const platformUpgradeTemplate = `
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: upgrade-cluster
  annotations:
    policy.open-cluster-management.io/standards: NIST SP 800-53
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
spec:
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: upgrade-cluster
        spec:
          remediationAction: inform
          severity: high
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterVersion
                metadata:
                  name: version
                spec:
                  channel: {{ .Channel }}
                  desiredUpdate:
{{ if .Version }}
                    version: {{ .Version }}
{{ end }}
{{ if .Image }}
                    image: {{ .Image }}
{{ end }}
{{ if eq .Force "true" }}
                    force: {{ .Force }}
{{ end }}
                  upstream: {{ .Upstream }}
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: policy-upgrade-check-co-available
        spec:
          remediationAction: inform
          severity: low
          namespaceSelector:
            exclude:
              - kube-*
            include:
              - default
          object-templates:
            - complianceType: mustnothave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterOperator
                status:
                  conditions:
                    - status: 'False'
                      type: Available
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: policy-upgrade-check-co-not-degraded
        spec:
          remediationAction: inform
          severity: low
          namespaceSelector:
            exclude:
              - kube-*
            include:
              - default
          object-templates:
            - complianceType: mustnothave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterOperator
                status:
                  conditions:
                    - status: 'True'
                      type: Degraded           
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: check-installed-version-status
        spec:
          remediationAction: inform # will be overridden by remediationAction in parent policy
          severity: high
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterVersion
                metadata:
                  name: version
                status:
                  history:
                    - state: Completed 
{{ if .Version }}
                      version: {{ .Version }}
{{ end }}
{{ if .Image }}
                      image: {{ .Image }}
{{ end }}
  remediationAction: inform     
`

const operatorUpgradeTemplate = `
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: upgrade-operator
  annotations:
    policy.open-cluster-management.io/standards: NIST SP 800-53
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
spec:
  remediationAction: inform
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: policy-subscription
        spec:
          remediationAction: enforce # the policy-template spec.remediationAction is overridden by the preceding parameter value for spec.remediationAction.
          severity: low
          namespaceSelector:
            exclude: ["kube-*"]
            include: ["*"]
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: operators.coreos.com/v1alpha1
                kind: Subscription
                metadata:
                  name: {{ .Name }}-upgrade
                  namespace: {{ . Namespace }}
                spec:
                  channel: "{{ .Channel }}"
                  installPlanApproval: Automatic
                  name: {{ .Name }}
                  source: "redhat-operators"
                  sourceNamespace: openshift-marketplace
`
