package utils

import (
	"bytes"
	"fmt"
	"strings"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// GenerateAbortManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for abort stage of an ImageBasedUpgrade
func GenerateAbortManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Idle
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
			Name: "idleConditionMessages",
			Path: `.status.conditions[?(@.type=="Idle")].message`,
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, "isIdle")
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GenerateFinalizeManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for finalize stage of an ImageBasedUpgrade
func GenerateFinalizeManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Idle
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
			Name: "idleConditionMessages",
			Path: `.status.conditions[?(@.type=="Idle")].message`,
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, "isIdle")
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GenerateUpgradeManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for upgrade stage of an ImageBasedUpgrade
func GenerateUpgradeManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Upgrade
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
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, "isUpgradeCompleted")
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GenerateRollbackManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for rollback stage of an ImageBasedUpgrade
func GenerateRollbackManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Rollback
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
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, "isRollbackCompleted")
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GeneratePrepManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for Prep stage of an ImageBasedUpgrade
func GeneratePrepManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade, manifests []mwv1.Manifest) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Prep
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
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1+len(manifests), "isPrepCompleted")
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, manifests)
}

// GeneratePermissionsManifestWorkReplicaset returns a ManifestWorkReplicaset for permissions required for work agent to access IBU
func GeneratePermissionsManifestWorkReplicaset(name, namespace string) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	clusterRole := &rbac.ClusterRole{
		ObjectMeta: v1.ObjectMeta{
			Name: "open-cluster-management:klusterlet-work:ibu-role",
			Labels: map[string]string{
				"open-cluster-management.io/aggregate-to-work": "true",
			},
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{"lca.openshift.io"},
				Resources: []string{"imagebasedupgrades"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
	crBytes, err := ObjectToByteArray(clusterRole)
	if err != nil {
		return nil, fmt.Errorf("failed to convert ClusterRole to []bytes: %w", err)
	}
	manifestConfigs := []mwv1.ManifestConfigOption{}
	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mwv1alpha1.ManifestWorkReplicaSetSpec{
			PlacementRefs: []mwv1alpha1.LocalPlacementReference{
				{
					Name: "dummy",
				},
			},
			ManifestWorkTemplate: mwv1.ManifestWorkSpec{
				Workload: mwv1.ManifestsTemplate{
					Manifests: []mwv1.Manifest{{RawExtension: runtime.RawExtension{Raw: crBytes}}},
				},
				DeleteOption:    &mwv1.DeleteOption{PropagationPolicy: mwv1.DeletePropagationPolicyTypeOrphan},
				ManifestConfigs: manifestConfigs,
			},
		},
	}
	return mwrs, nil
}

func generateManifestWorkReplicaset(name, namespace, expectedValueAnn string,
	jsonPaths []mwv1.JsonPath, ibu *lcav1.ImageBasedUpgrade, extraManifests []mwv1.Manifest) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibuRaw, err := ibuToBytes(ibu)
	if err != nil {
		return nil, err
	}
	clusterRole := &rbac.ClusterRole{
		ObjectMeta: v1.ObjectMeta{
			Name: "open-cluster-management:klusterlet-work:ibu-role",
			Labels: map[string]string{
				"open-cluster-management.io/aggregate-to-work": "true",
			},
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{"lca.openshift.io"},
				Resources: []string{"imagebasedupgrades"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
	crBytes, err := ObjectToByteArray(clusterRole)
	if err != nil {
		return nil, fmt.Errorf("failed to convert ClusterRole to []bytes: %w", err)
	}
	manifestConfigs := []mwv1.ManifestConfigOption{
		{
			ResourceIdentifier: mwv1.ResourceIdentifier{
				Name:     ibu.GetName(),
				Group:    ibu.GroupVersionKind().Group,
				Resource: "imagebasedupgrades",
			},
			FeedbackRules: []mwv1.FeedbackRule{
				{
					Type:      mwv1.JSONPathsType,
					JsonPaths: jsonPaths,
				},
			},
		},
	}
	manifests := append(extraManifests,
		mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: crBytes}},
		mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: ibuRaw}})

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
					Manifests: manifests,
				},
				DeleteOption:    &mwv1.DeleteOption{PropagationPolicy: mwv1.DeletePropagationPolicyTypeOrphan},
				ManifestConfigs: manifestConfigs,
			},
		},
	}
	return mwrs, nil
}

func ibuToBytes(ibu *lcav1.ImageBasedUpgrade) ([]byte, error) {
	scheme := runtime.NewScheme()
	lcav1.AddToScheme(scheme)
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

// GenerateClusterGroupUpgradeForPlanItem returns a populated CGU for a PlanItem
func GenerateClusterGroupUpgradeForPlanItem(
	name string, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, planItem *ibguv1alpha1.PlanItem,
	templateNames, blockingCGUs []string, annotations map[string]string,
) *ranv1alpha1.ClusterGroupUpgrade {
	blockingCRs := []ranv1alpha1.BlockingCR{}
	for _, cguName := range blockingCGUs {
		blockingCRs = append(blockingCRs, ranv1alpha1.BlockingCR{
			Name:      cguName,
			Namespace: ibgu.GetNamespace(),
		})
	}
	enable := true
	beforeEnable := ranv1alpha1.BeforeEnable{
		AddClusterAnnotations: map[string]string{
			"import.open-cluster-management.io/disable-auto-import": "true",
		},
	}
	afterCompletion := ranv1alpha1.AfterCompletion{
		RemoveClusterAnnotations: []string{"import.open-cluster-management.io/disable-auto-import"},
	}

	return &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:        name,
			Namespace:   ibgu.GetNamespace(),
			Annotations: annotations,
			Labels: map[string]string{
				CGUOwnerIBGULabel: ibgu.GetName(),
			},
		},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
			ClusterLabelSelectors: ibgu.Spec.ClusterLabelSelectors,
			Enable:                &enable,
			ManifestWorkTemplates: templateNames,
			RemediationStrategy: &ranv1alpha1.RemediationStrategySpec{
				MaxConcurrency: planItem.RolloutStrategy.MaxConcurrency,
				Timeout:        planItem.RolloutStrategy.Timeout,
			},
			BlockingCRs: blockingCRs,
			Actions: ranv1alpha1.Actions{
				BeforeEnable:    &beforeEnable,
				AfterCompletion: &afterCompletion,
			},
		},
	}
}

// GetActionFromMWRSName returns the ImageBasedUpgradeAction corresponding to the mwrs template name
func GetActionFromMWRSName(mwrsName string) string {
	splitted := strings.Split(mwrsName, "-")
	last := splitted[len(splitted)-1]
	actions := []string{
		ibguv1alpha1.Abort, ibguv1alpha1.Finalize, ibguv1alpha1.Upgrade, ibguv1alpha1.Rollback, ibguv1alpha1.Prep,
	}
	for _, action := range actions {
		if strings.EqualFold(last, action) {
			return action
		}
	}
	return ""
}

// GetAllActionMessagesFromCGU returns the list of all actions based on CGU's ManifestWorkTemplates
func GetAllActionMessagesFromCGU(cgu *ranv1alpha1.ClusterGroupUpgrade) []ibguv1alpha1.ActionMessage {
	return getActionMessagesFromCGU(cgu, -1)
}

// GetFirstNActionMessagesFromCGU returns the list of n first actions based on CGU's ManifestWorkTemplates
func GetFirstNActionMessagesFromCGU(cgu *ranv1alpha1.ClusterGroupUpgrade, count int) []ibguv1alpha1.ActionMessage {
	return getActionMessagesFromCGU(cgu, count)
}

func getActionMessagesFromCGU(cgu *ranv1alpha1.ClusterGroupUpgrade, limit int) []ibguv1alpha1.ActionMessage {
	actions := make([]ibguv1alpha1.ActionMessage, 0)
	for i, manifest := range cgu.Spec.ManifestWorkTemplates {
		if limit >= 0 && i >= limit {
			break
		}
		action := GetActionFromMWRSName(manifest)
		if action == "" {
			continue
		}
		actions = append(actions, ibguv1alpha1.ActionMessage{Action: action})
	}
	return actions
}

// GetConditionMessageFromManifestWorkStatus return the final message of a manifest work status for ibu
func GetConditionMessageFromManifestWorkStatus(status *ranv1alpha1.ManifestWorkStatus) string {
	if status == nil {
		return ""
	}
	if len(status.Status.Manifests) == 0 {
		return ""
	}
	for _, value := range status.Status.Manifests[0].StatusFeedbacks.Values {
		if strings.Contains(value.Name, "CompletedConditionMessages") {
			if value.Value.String != nil {
				return *value.Value.String
			}
		}
	}
	return ""
}
