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

// PlanItemClusterSelectors returns the clusterLabelSelectors a plan item's CGU
// receives: the frozen cluster set as a `name In (...)` requirement, plus the plan
// item's own action requirement. The CGU unions its selector entries, so every entry
// carries the frozen set and the union can only select clusters inside it — a cluster
// added after the freeze does not join, and a removed one simply stops matching.
//
// The action requirement is keyed on the plan item's first action, the same predicate
// table the CGU used before IBGU resolved cluster lists itself. A disjunctive
// requirement — AbortOnFailure targets clusters that failed Prep *or* Upgrade — needs
// one entry per disjunct, because a single LabelSelector ANDs its requirements.
//
// frozenClusters must be non-empty: an `In` requirement with no values is not a valid
// selector.
func PlanItemClusterSelectors(frozenClusters []string, planItem *ibguv1alpha1.PlanItem) []metav1.LabelSelector {
	frozen := metav1.LabelSelectorRequirement{
		Key:      ClusterNameLabel,
		Operator: metav1.LabelSelectorOpIn,
		Values:   frozenClusters,
	}
	exists := func(key string) metav1.LabelSelectorRequirement {
		return metav1.LabelSelectorRequirement{Key: key, Operator: metav1.LabelSelectorOpExists}
	}
	completed := func(action string) string {
		return fmt.Sprintf(IBGUActionCompletedLabelTemplate, strings.ToLower(action))
	}
	failed := func(action string) string {
		return fmt.Sprintf(IBGUActionFailedLabelTemplate, strings.ToLower(action))
	}

	// One requirement group per disjunct; independent groups are unioned by the CGU.
	var groups [][]metav1.LabelSelectorRequirement
	if len(planItem.Actions) == 0 {
		groups = [][]metav1.LabelSelectorRequirement{{}}
	} else {
		switch planItem.Actions[0] {
		case ibguv1alpha1.Abort:
			groups = [][]metav1.LabelSelectorRequirement{{{
				Key:      completed(ibguv1alpha1.Upgrade),
				Operator: metav1.LabelSelectorOpDoesNotExist,
			}}}
		case ibguv1alpha1.Rollback, ibguv1alpha1.FinalizeUpgrade:
			groups = [][]metav1.LabelSelectorRequirement{{exists(completed(ibguv1alpha1.Upgrade))}}
		case ibguv1alpha1.FinalizeRollback:
			groups = [][]metav1.LabelSelectorRequirement{{exists(completed(ibguv1alpha1.Rollback))}}
		case ibguv1alpha1.AbortOnFailure:
			groups = [][]metav1.LabelSelectorRequirement{
				{exists(failed(ibguv1alpha1.Prep))},
				{exists(failed(ibguv1alpha1.Upgrade))},
			}
		case ibguv1alpha1.Upgrade:
			groups = [][]metav1.LabelSelectorRequirement{{exists(completed(ibguv1alpha1.Prep))}}
		default:
			// Prep-first and composite items, and any unrecognised first action, act on
			// the whole frozen set.
			groups = [][]metav1.LabelSelectorRequirement{{}}
		}
	}

	selectors := make([]metav1.LabelSelector, 0, len(groups))
	for _, group := range groups {
		requirements := make([]metav1.LabelSelectorRequirement, 0, len(group)+1)
		requirements = append(requirements, frozen)
		requirements = append(requirements, group...)
		selectors = append(selectors, metav1.LabelSelector{MatchExpressions: requirements})
	}
	return selectors
}

// GenerateClusterGroupUpgradeForPlanItem returns a populated CGU for a PlanItem.
// The cluster set is resolved and frozen by the IBGU reconciler and passed in as
// clusterLabelSelectors that carry it — see PlanItemClusterSelectors — so the CGU
// targets spec.clusterLabelSelectors rather than an explicit spec.clusters list.
func GenerateClusterGroupUpgradeForPlanItem(
	name string, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, planItem *ibguv1alpha1.PlanItem,
	templateNames []string, clusterSelectors []metav1.LabelSelector, annotations map[string]string, disableAutoImport bool,
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
			ClusterLabelSelectors: clusterSelectors,
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
