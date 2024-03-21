package templates

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

// AztpTemplateData holds data for AZTP template rendering
type AztpTemplateData struct {
	AztpImage string
}

const (
	aztpDeployServiceVariant = "full"
)

var fullVariantemplates = []string{namespace, serviceAccount, clusterRole, clusterRoleBinding, job}
var policiesVariantemplates = []string{namespace}

func renderYamlTemplate(
	resourceName string,
	templateBody string,
	data AztpTemplateData) (*bytes.Buffer, error) {

	w := new(bytes.Buffer)
	template, err := template.New(resourceName).Parse(templateBody)
	if err != nil {
		return w, fmt.Errorf("failed to parse template %s: %v", resourceName, err)
	}

	err = template.Execute(w, data)
	if err != nil {
		return w, fmt.Errorf("failed to render template %s: %v", resourceName, err)
	}
	return w, nil
}

// RenderAztpService renders AZTP templates for post-reboot service
func RenderAztpService(data AztpTemplateData, variant string) ([]unstructured.Unstructured, error) {
	var templates []string
	if strings.Compare(variant, aztpDeployServiceVariant) == 0 {
		templates = fullVariantemplates
	} else {
		templates = policiesVariantemplates
	}
	var objects = make([]unstructured.Unstructured, len(templates))
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	for idx, t := range templates {
		buf, err := renderYamlTemplate(fmt.Sprint(idx), t, data)
		if err != nil {
			return objects, err
		}
		obj := &unstructured.Unstructured{}
		_, _, err = dec.Decode(buf.Bytes(), nil, obj)
		if err != nil {
			return objects, err
		}
		objects[idx] = *obj.DeepCopy()

	}
	return objects, nil
}

const namespace string = `
apiVersion: v1
kind: Namespace
metadata:
  name: ztp-profile

`

const serviceAccount string = `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ztp-profile-accelerator-sa
  namespace: ztp-profile

`
const clusterRole string = `
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: ztp-profile-accelerator-clusterrole
rules:
# AZTP job must be able to manipulate any resource
# (same as ACM configuration policy controller)
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]

`

const clusterRoleBinding string = `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ztp-profile-accelerator-crb
  namespace: ztp-profile
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ztp-profile-accelerator-clusterrole
subjects:
- kind: ServiceAccount
  name: ztp-profile-accelerator-sa
  namespace: ztp-profile

`

const job string = `
apiVersion: batch/v1
kind: Job
metadata:
  name: ztp-profile-install-accelerator
  namespace: ztp-profile
spec:
  backoffLimit: 2
  template:
    spec:
      serviceAccountName: ztp-profile-accelerator-sa
      terminationGracePeriodSeconds: 3
      nodeSelector:
        node-role.kubernetes.io/master: ""
      restartPolicy: OnFailure
      containers:
        - name: ztp-accelerator
          securityContext:
            allowPrivilegeEscalation: false
            seccompProfile:
              type: RuntimeDefault
            capabilities:
              drop:
              - ALL
          image: {{ .AztpImage }}
          imagePullPolicy: Always
          command:
          - /aztp
          env:
          - name: CONFIGMAP_NAME
            value: "ztp-post-provision"
          - name: CONFIGMAP_NAMESPACE
            value: "ztp-profile"
          - name: END_CONDITION_EXTENSION_TIME
            value: 60s
`
