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
	"k8s.io/apimachinery/pkg/runtime"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// conditionFieldPath builds the feedback JSONPath for a field of an IBU status
// condition, e.g. `.status.conditions[?(@.type=="UpgradeCompleted")].status`.
// Sourcing the condition type from lcav1.ConditionTypes links the feedback paths
// to LCA's public API at compile time: a renamed IBU condition type becomes a
// build error at vendor-bump time instead of a silent runtime stall.
func conditionFieldPath(ct lcav1.ConditionType, field string) string {
	return fmt.Sprintf(`.status.conditions[?(@.type==%q)].%s`, string(ct), field)
}

// isCompletedFeedbackName returns the feedback-rule name for a stage's
// completion-status check, e.g. "isUpgradeCompleted". The generator and the
// report-only stage-failure detector both derive the name through this helper so
// they always agree on it.
func isCompletedFeedbackName(ct lcav1.ConditionType) string {
	return "is" + string(ct)
}

// GenerateAbortManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for abort stage of an ImageBasedUpgrade
func GenerateAbortManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Idle
	jsonPaths := []mwv1.JsonPath{
		{
			Name: isCompletedFeedbackName(lcav1.ConditionTypes.Idle),
			Path: conditionFieldPath(lcav1.ConditionTypes.Idle, "status"),
		},
		{
			Name: "idleConditionReason",
			Path: conditionFieldPath(lcav1.ConditionTypes.Idle, "reason"),
		},
		{
			Name: "idleConditionMessages",
			Path: conditionFieldPath(lcav1.ConditionTypes.Idle, "message"),
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, isCompletedFeedbackName(lcav1.ConditionTypes.Idle))
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GenerateFinalizeManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for finalize stage of an ImageBasedUpgrade
func GenerateFinalizeManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Idle
	jsonPaths := []mwv1.JsonPath{
		{
			Name: isCompletedFeedbackName(lcav1.ConditionTypes.Idle),
			Path: conditionFieldPath(lcav1.ConditionTypes.Idle, "status"),
		},
		{
			Name: "idleConditionReason",
			Path: conditionFieldPath(lcav1.ConditionTypes.Idle, "reason"),
		},
		{
			Name: "idleConditionMessages",
			Path: conditionFieldPath(lcav1.ConditionTypes.Idle, "message"),
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, isCompletedFeedbackName(lcav1.ConditionTypes.Idle))
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GenerateUpgradeManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for upgrade stage of an ImageBasedUpgrade
func GenerateUpgradeManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Upgrade
	jsonPaths := []mwv1.JsonPath{
		{
			Name: isCompletedFeedbackName(lcav1.ConditionTypes.UpgradeCompleted),
			Path: conditionFieldPath(lcav1.ConditionTypes.UpgradeCompleted, "status"),
		},
		{
			Name: "upgradeInProgressConditionMessage",
			Path: conditionFieldPath(lcav1.ConditionTypes.UpgradeInProgress, "message"),
		},
		{
			Name: "upgradeCompletedConditionMessages",
			Path: conditionFieldPath(lcav1.ConditionTypes.UpgradeCompleted, "message"),
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, isCompletedFeedbackName(lcav1.ConditionTypes.UpgradeCompleted))
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GenerateRollbackManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for rollback stage of an ImageBasedUpgrade
func GenerateRollbackManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Rollback
	jsonPaths := []mwv1.JsonPath{
		{
			Name: isCompletedFeedbackName(lcav1.ConditionTypes.RollbackCompleted),
			Path: conditionFieldPath(lcav1.ConditionTypes.RollbackCompleted, "status"),
		},
		{
			Name: "rollbackInProgressConditionMessage",
			Path: conditionFieldPath(lcav1.ConditionTypes.RollbackInProgress, "message"),
		},
		{
			Name: "rollbackCompletedConditionMessages",
			Path: conditionFieldPath(lcav1.ConditionTypes.RollbackCompleted, "message"),
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1, isCompletedFeedbackName(lcav1.ConditionTypes.RollbackCompleted))
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, []mwv1.Manifest{})
}

// GeneratePrepManifestWorkReplicaset returns a populated ManifestWorkReplicaSet for Prep stage of an ImageBasedUpgrade
func GeneratePrepManifestWorkReplicaset(name, namespace string, ibu *lcav1.ImageBasedUpgrade, manifests []mwv1.Manifest) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	ibu.Spec.Stage = lcav1.Stages.Prep
	jsonPaths := []mwv1.JsonPath{
		{
			Name: isCompletedFeedbackName(lcav1.ConditionTypes.PrepCompleted),
			Path: conditionFieldPath(lcav1.ConditionTypes.PrepCompleted, "status"),
		},
		{
			Name: "prepInProgressConditionMessage",
			Path: conditionFieldPath(lcav1.ConditionTypes.PrepInProgress, "message"),
		},
		{
			Name: "prepCompletedConditionMessages",
			Path: conditionFieldPath(lcav1.ConditionTypes.PrepCompleted, "message"),
		},
	}
	expectedValueAnn := fmt.Sprintf(manifestWorkExpectedValuesAnnotationTemplate, 1+len(manifests), isCompletedFeedbackName(lcav1.ConditionTypes.PrepCompleted))
	return generateManifestWorkReplicaset(name, namespace, expectedValueAnn, jsonPaths, ibu, manifests)
}

// ibuRoleClusterRole returns the open-cluster-management:klusterlet-work:ibu-role
// ClusterRole granting the spoke work-agent the permissions it needs to apply the
// objects TALM copies into a Prep ManifestWork: the ImageBasedUpgrade itself and
// the per-reference BackupContent copies. It is defined once and shared by every
// generator so the two permission sets cannot drift.
func ibuRoleClusterRole() *rbac.ClusterRole {
	return &rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
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
			{
				APIGroups: []string{"lca.openshift.io"},
				Resources: []string{"backupcontents"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
}

// GeneratePermissionsManifestWorkReplicaset returns a ManifestWorkReplicaset for permissions required for work agent to access IBU
func GeneratePermissionsManifestWorkReplicaset(name, namespace string) (*mwv1alpha1.ManifestWorkReplicaSet, error) {
	clusterRole := ibuRoleClusterRole()
	crBytes, err := ObjectToByteArray(clusterRole)
	if err != nil {
		return nil, fmt.Errorf("failed to convert ClusterRole to []bytes: %w", err)
	}
	manifestConfigs := []mwv1.ManifestConfigOption{}
	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
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
	clusterRole := ibuRoleClusterRole()
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

	// The ClusterRole must precede the extra manifests so the spoke work-agent has
	// the backupcontents/imagebasedupgrades permissions before it applies the copied
	// content, and the IBU stays last. GeneratePrepManifestWorkReplicaset annotates
	// the completion index as 1+len(extraManifests) (the IBU's 0-based position),
	// which this ordering preserves.
	manifests := make([]mwv1.Manifest, 0, len(extraManifests)+2)
	manifests = append(manifests, mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: crBytes}})
	manifests = append(manifests, extraManifests...)
	manifests = append(manifests, mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: ibuRaw}})

	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
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
	err := lcav1.AddToScheme(scheme)
	if err != nil {
		return []byte{}, err
	}

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
	err = s.Encode(ibu, &dst)
	return dst.Bytes(), err
}

// ClusterMatchesPlanItem reports whether a ManagedCluster carrying the given
// labels is eligible for the plan item. IBGU resolves the cluster set once and
// evaluates per-plan-item eligibility here, against the labels the caller passes
// via effectiveActionLabels(...) — the ManagedCluster labels merged with the
// IBGU's freshly synced status — instead of handing a narrowed label selector to
// each generated CGU. The narrowing is keyed on the plan item's first action (the
// switch below). Prep-first items and any unrecognised first action match the
// whole base set.
func ClusterMatchesPlanItem(clusterLabels map[string]string, planItem *ibguv1alpha1.PlanItem) bool {
	if len(planItem.Actions) == 0 {
		return true
	}
	hasCompleted := func(action string) bool {
		_, ok := clusterLabels[fmt.Sprintf(IBGUActionCompletedLabelTemplate, strings.ToLower(action))]
		return ok
	}
	hasFailed := func(action string) bool {
		_, ok := clusterLabels[fmt.Sprintf(IBGUActionFailedLabelTemplate, strings.ToLower(action))]
		return ok
	}
	switch planItem.Actions[0] {
	case ibguv1alpha1.Abort:
		return !hasCompleted(ibguv1alpha1.Upgrade)
	case ibguv1alpha1.Rollback, ibguv1alpha1.FinalizeUpgrade:
		return hasCompleted(ibguv1alpha1.Upgrade)
	case ibguv1alpha1.FinalizeRollback:
		return hasCompleted(ibguv1alpha1.Rollback)
	case ibguv1alpha1.AbortOnFailure:
		return hasFailed(ibguv1alpha1.Prep) || hasFailed(ibguv1alpha1.Upgrade)
	case ibguv1alpha1.Upgrade:
		return hasCompleted(ibguv1alpha1.Prep)
	default:
		return true
	}
}

// GenerateClusterGroupUpgradeForPlanItem returns a populated CGU for a PlanItem.
// The cluster set is resolved and frozen by the IBGU reconciler and passed in as
// an explicit clusters list, so the generated CGU targets spec.clusters rather
// than a (per-plan-item narrowed) spec.clusterLabelSelectors.
func GenerateClusterGroupUpgradeForPlanItem(
	name string, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, planItem *ibguv1alpha1.PlanItem,
	templateNames, clusters []string, annotations map[string]string, disableAutoImport bool,
) *ranv1alpha1.ClusterGroupUpgrade {
	enable := true
	beforeEnable := ranv1alpha1.BeforeEnable{}
	afterCompletion := ranv1alpha1.AfterCompletion{}
	if disableAutoImport {
		beforeEnable = ranv1alpha1.BeforeEnable{
			AddClusterAnnotations: map[string]string{
				"import.open-cluster-management.io/disable-auto-import": "true",
			},
		}
		afterCompletion = ranv1alpha1.AfterCompletion{
			RemoveClusterAnnotations: []string{"import.open-cluster-management.io/disable-auto-import"},
		}
	}

	return &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ibgu.GetNamespace(),
			Labels: map[string]string{
				CGUOwnerIBGULabel: ibgu.GetName(),
			},
			Annotations: annotations,
		},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
			Clusters:              clusters,
			Enable:                &enable,
			ManifestWorkTemplates: templateNames,
			RemediationStrategy: &ranv1alpha1.RemediationStrategySpec{
				MaxConcurrency: planItem.RolloutStrategy.MaxConcurrency,
				Timeout:        planItem.RolloutStrategy.Timeout,
			},
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
		ibguv1alpha1.Abort, ibguv1alpha1.FinalizeUpgrade, ibguv1alpha1.Upgrade,
		ibguv1alpha1.FinalizeRollback, ibguv1alpha1.AbortOnFailure,
		ibguv1alpha1.Rollback, ibguv1alpha1.Prep,
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
	msgs := []string{}
	for _, manifest := range status.Status.Manifests {
		for _, value := range manifest.StatusFeedbacks.Values {
			if strings.Contains(value.Name, "ConditionMessage") {
				if value.Value.String != nil {
					msgs = append(msgs, *value.Value.String)
				}
			}
			if strings.Contains(value.Name, "ConditionReason") {
				if value.Value.String != nil {
					msgs = append(msgs, *value.Value.String)
				}
			}
		}

		for _, condition := range manifest.Conditions {
			if condition.Type == "Applied" && condition.Status == "False" {
				msgs = append(msgs, condition.Message)
			}
		}
	}
	return strings.Join(msgs, "\n")
}
