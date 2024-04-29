package utils

import (
	"bytes"
	"fmt"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	lcav1alpha1 "github.com/openshift-kni/lifecycle-agent/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// ManifestWorkReplicaSet names for different actions
const (
	ManifestWorkReplicaSetAbortName    = "ibu-abort"
	ManifestWorkReplicaSetFinalizeName = "ibu-finalize"
	ManifestWorkReplicaSetUpgradeName  = "ibu-upgrade"
	ManifestWorkReplicaSetPrepName     = "ibu-prep"
	ManifestWorkReplicaSetRollbackName = "ibu-rollback"
)

func abortManifestWorkReplicaset(namespace string, ibu *lcav1alpha1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1alpha1.Stages.Idle
	jsonPaths := []mwv1.JsonPath{
		{
			Name: "isIdle",
			Path: `.status.conditions[?(@.type=="Idle")].status`,
		},
		{
			Name: "idleConditionReason",
			Path: `.status.conditions[?(@.type=="Idle")].reason'`,
		},
		{
			Name: "idleConditionMessage",
			Path: `.status.conditions[?(@.type=="Idle")].message`,
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, "isIdle")
	return generateManifestWorkReplicaset(ManifestWorkReplicaSetAbortName, namespace, expectedValueAnn, jsonPaths, ibu)
}

func finalizeManifestWorkReplicaset(namespace string, ibu *lcav1alpha1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1alpha1.Stages.Idle
	jsonPaths := []mwv1.JsonPath{
		{
			Name: "isIdle",
			Path: `.status.conditions[?(@.type=="Idle")].status`,
		},
		{
			Name: "idleConditionReason",
			Path: `.status.conditions[?(@.type=="Idle")].reason'`,
		},
		{
			Name: "idleConditionMessage",
			Path: `.status.conditions[?(@.type=="Idle")].message`,
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, "isIdle")
	return generateManifestWorkReplicaset(ManifestWorkReplicaSetFinalizeName, namespace, expectedValueAnn, jsonPaths, ibu)
}

func upgradeManifestWorkReplicaset(namespace string, ibu *lcav1alpha1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1alpha1.Stages.Upgrade
	jsonPaths := []mwv1.JsonPath{
		{
			Name: "isUpgradeCompleted",
			Path: `.status.conditions[?(@.type=="UpgradeCompleted")].status`,
		},
		{
			Name: "upgradeInProgressConditionMessage",
			Path: `.status.conditions[?(@.type=="UpgradeInProgress")].message'`,
		},
		{
			Name: "upgradeCompletedConditionMessages",
			Path: `.status.conditions[?(@.type=="UpgradeCompleted")].message`,
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, "isUpgradeCompleted")
	return generateManifestWorkReplicaset(ManifestWorkReplicaSetUpgradeName, namespace, expectedValueAnn, jsonPaths, ibu)
}

func rollbackManifestWorkReplicaset(namespace string, ibu *lcav1alpha1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1alpha1.Stages.Rollback
	jsonPaths := []mwv1.JsonPath{
		{
			Name: "isRollbackCompleted",
			Path: `.status.conditions[?(@.type=="RollbackCompleted")].status`,
		},
		{
			Name: "rollbackInProgressConditionMessage",
			Path: `.status.conditions[?(@.type=="RollbackInProgress")].message'`,
		},
		{
			Name: "rollbackCompletedConditionMessages",
			Path: `.status.conditions[?(@.type=="RollbackCompleted")].message`,
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, "isRollbackCompleted")
	return generateManifestWorkReplicaset(ManifestWorkReplicaSetPrepName, namespace, expectedValueAnn, jsonPaths, ibu)
}

func prepManifestWorkReplicaset(namespace string, ibu *lcav1alpha1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1alpha1.Stages.Prep
	jsonPaths := []mwv1.JsonPath{
		{
			Name: "isPrepCompleted",
			Path: `.status.conditions[?(@.type=="PrepCompleted")].status`,
		},
		{
			Name: "prepInProgressConditionMessage",
			Path: `.status.conditions[?(@.type=="PrepInProgress")].message'`,
		},
		{
			Name: "prepCompletedConditionMessages",
			Path: `.status.conditions[?(@.type=="PrepCompleted")].message`,
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, "isPrepCompleted")
	return generateManifestWorkReplicaset(ManifestWorkReplicaSetPrepName, namespace, expectedValueAnn, jsonPaths, ibu)
}

func generateManifestWorkReplicaset(name, namespace, expectedValueAnn string, jsonPaths []mwv1.JsonPath, ibu *lcav1alpha1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibuRaw, err := ibuToBytes(ibu)
	if err != nil {
		return nil, err
	}
	manifestConfigs := []mwv1.ManifestConfigOption{
		{
			ResourceIdentifier: mwv1.ResourceIdentifier{
				Name:     ibu.GetName(),
				Group:    ibu.GroupVersionKind().Group,
				Resource: ibu.GetResourceVersion(),
			},
			FeedbackRules: []mwv1.FeedbackRule{
				{
					Type:      mwv1.JSONPathsType,
					JsonPaths: jsonPaths,
				},
			},
		},
	}
	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				manifestWorkExpectedValuesAnnotation: expectedValueAnn,
			},
		},
		Spec: mwv1alpha1.ManifestWorkReplicaSetSpec{
			PlacementRefs: []mwv1alpha1.LocalPlacementReference{
				{
					Name: "dummy",
				},
			},
			ManifestWorkTemplate: mwv1.ManifestWorkSpec{
				Workload: mwv1.ManifestsTemplate{
					Manifests: []mwv1.Manifest{{RawExtension: runtime.RawExtension{Raw: ibuRaw}}},
				},
				DeleteOption:    &mwv1.DeleteOption{},
				ManifestConfigs: manifestConfigs,
			},
		},
	}
	return mwrs, nil
}

func ibuToBytes(ibu *lcav1alpha1.ImageBasedUpgrade) ([]byte, error) {
	scheme := runtime.NewScheme()
	lcav1alpha1.AddToScheme(scheme)
	s := serializer.NewSerializerWithOptions(serializer.DefaultMetaFactory, scheme, scheme, serializer.SerializerOptions{})
	gvks, isUnversioned, err := scheme.ObjectKinds(ibu)
	if err != nil {
		return []byte{}, err
	}
	if !isUnversioned && len(gvks) == 1 {
		ibu.TypeMeta = metav1.TypeMeta{
			Kind:       gvks[0].Kind,
			APIVersion: gvks[0].GroupVersion().Identifier(),
		}
	}
	var dst bytes.Buffer
	s.Encode(ibu, &dst)
	return dst.Bytes(), nil
}

func getClusterGroupUpgradeForCGIBU(cgibu *ranv1alpha1.ClusterGroupImageBasedUpgrade) *ranv1alpha1.ClusterGroupUpgrade {
	enable := true
	manifestWorkTemplates := []string{}
	for _, action := range cgibu.Spec.Actions {
		templateName := ""
		switch action {
		case ranv1alpha1.IBUActions.Prep:
			templateName = ManifestWorkReplicaSetPrepName
		case ranv1alpha1.IBUActions.Upgrade:
			templateName = ManifestWorkReplicaSetUpgradeName
		case ranv1alpha1.IBUActions.Abort:
			templateName = ManifestWorkReplicaSetAbortName
		case ranv1alpha1.IBUActions.Finalize:
			templateName = ManifestWorkReplicaSetFinalizeName
		case ranv1alpha1.IBUActions.Rollback:
			templateName = ManifestWorkReplicaSetRollbackName
		}

		manifestWorkTemplates = append(manifestWorkTemplates, templateName)
	}
	cgu := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "ibu-upgrade",
			Namespace: cgibu.GetNamespace(),
		},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
			ClusterLabelSelectors: cgibu.Spec.ClusterLabelSelectors,
			Enable:                &enable,
			ManifestWorkTemplates: manifestWorkTemplates,
			RemediationStrategy: &ranv1alpha1.RemediationStrategySpec{
				MaxConcurrency: cgibu.Spec.RolloutStrategy.MaxConcurrency,
				Timeout:        cgibu.Spec.RolloutStrategy.Timeout,
			},
		},
	}
	return cgu
}
