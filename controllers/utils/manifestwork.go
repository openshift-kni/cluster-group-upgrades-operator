package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// terminalCompletionConditions lists the <Stage>Completed IBU condition types for
// which a "False" feedback value is a genuine terminal stage failure. Idle is
// deliberately excluded: its condition reports "False" for the whole normal
// duration of an Abort/Finalize and only flips to "True" on completion, so
// Idle=False is not a failure.
var terminalCompletionConditions = []lcav1.ConditionType{
	lcav1.ConditionTypes.PrepCompleted,
	lcav1.ConditionTypes.UpgradeCompleted,
	lcav1.ConditionTypes.RollbackCompleted,
}

const manifestWorkExpectedValuesAnnotation = "openshift-cluster-group-upgrades/expectedValues"
const manifestWorkExpectedValuesAnnotationTemplate = `[{"manifestIndex":%d,"name":"%s","value":"True"}]`

// This type is not exposed by mwv1 unfortunately, copied from:
// https://github.com/open-cluster-management-io/work/blob/81fc808f78ce4dafa9c24f979af4e33078df48b6/pkg/spoke/controllers/statuscontroller/availablestatus_controller.go#L30
const statusFeedbackConditionType = "StatusFeedbackSynced"

// ManifestWorkExpectedValues defines the expected values for
// the fields synced back from the spoke through feedback rules.
type ManifestWorkExpectedValues []struct {
	ManifestIndex int32  `json:"manifestIndex,omitempty"`
	Name          string `json:"name,omitempty"`
	Value         string `json:"value,omitempty"`
}

func getManifestWorkName(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, startIndex int) string {
	return GetSafeResourceName(clusterGroupUpgrade.Namespace+"."+clusterGroupUpgrade.Spec.ManifestWorkTemplates[startIndex], "", clusterGroupUpgrade, MaxObjectNameLength)
}

// IsManifestWorkCompleted returns true if the manifestwork is applied and the field values match the expectation
func IsManifestWorkCompleted(mw *mwv1.ManifestWork) (bool, error) {
	if !isManifestWorkReady(mw) {
		return false, nil
	}
	expectedValuesString := mw.Annotations[manifestWorkExpectedValuesAnnotation]
	if expectedValuesString != "" {
		expectedValues := ManifestWorkExpectedValues{}
		if err := json.Unmarshal([]byte(expectedValuesString), &expectedValues); err != nil {
			return false, err
		}
		for _, expectedValue := range expectedValues {
			mc := getManifestCondition(mw, expectedValue.ManifestIndex)
			if mc == nil || !IsManifestConditionReady(mc) || expectedValue.Value != getFieldValue(mc, expectedValue.Name) {
				return false, nil
			}
		}
	}
	return true, nil
}

func isManifestWorkReady(mw *mwv1.ManifestWork) bool {
	var applied, available bool
	for _, condition := range mw.Status.Conditions {
		if condition.Type == mwv1.ManifestApplied && condition.Status == v1.ConditionTrue {
			applied = true
		}
		if condition.Type == mwv1.ManifestAvailable && condition.Status == v1.ConditionTrue {
			available = true
		}
	}
	return applied && available
}

func getManifestCondition(mw *mwv1.ManifestWork, index int32) *mwv1.ManifestCondition {
	for _, manifestCondition := range mw.Status.ResourceStatus.Manifests {
		if manifestCondition.ResourceMeta.Ordinal == index {
			return &manifestCondition
		}
	}
	return nil
}

func getFieldValue(mc *mwv1.ManifestCondition, name string) string {
	for _, fieldValue := range mc.StatusFeedbacks.Values {
		if fieldValue.Name == name {
			switch fieldValue.Value.Type {
			case mwv1.String:
				return *fieldValue.Value.String
			case mwv1.Boolean:
				return strconv.FormatBool(*fieldValue.Value.Boolean)
			case mwv1.Integer:
				return strconv.FormatInt(*fieldValue.Value.Integer, 10)
			case mwv1.JsonRaw:
				return *fieldValue.Value.JsonRaw
			}
		}
	}
	return ""
}

// IsManifestConditionReady returns true if the manifest is applied, available and synced
func IsManifestConditionReady(mc *mwv1.ManifestCondition) bool {
	var applied, available, synced bool
	for _, condition := range mc.Conditions {
		if condition.Type == mwv1.ManifestApplied && condition.Status == v1.ConditionTrue {
			applied = true
		}
		if condition.Type == mwv1.ManifestAvailable && condition.Status == v1.ConditionTrue {
			available = true
		}
		if condition.Type == statusFeedbackConditionType && condition.Status == v1.ConditionTrue {
			synced = true
		}
	}
	return applied && available && synced
}

// GetStageFailureFromManifestWork reports whether LCA has terminally failed the
// current stage on the spoke and returns the LCA failure message. A failure is a
// <Stage>Completed=False feedback value (for a stage in terminalCompletionConditions)
// on a manifest that is applied, available and has synced its feedback.
//
// Absent feedback and ManifestApplied=False are NOT treated as failures: an
// unsynced or not-yet-created condition just means the stage is still in progress.
// This is report-only; it does not change the CGU state machine. The returned
// message is sourced from the ManifestWork feedback, falling back to a generic
// message when LCA reported no detail.
func GetStageFailureFromManifestWork(mw *mwv1.ManifestWork) (bool, string) {
	if mw == nil {
		return false, ""
	}
	watched := make(map[string]lcav1.ConditionType, len(terminalCompletionConditions))
	for _, ct := range terminalCompletionConditions {
		watched[isCompletedFeedbackName(ct)] = ct
	}
	for i := range mw.Status.ResourceStatus.Manifests {
		mc := &mw.Status.ResourceStatus.Manifests[i]
		// Only trust a manifest whose feedback is applied, available and synced.
		if !IsManifestConditionReady(mc) {
			continue
		}
		for _, fieldValue := range mc.StatusFeedbacks.Values {
			ct, ok := watched[fieldValue.Name]
			if !ok {
				continue
			}
			if fieldValue.Value.Type != mwv1.String || fieldValue.Value.String == nil {
				continue
			}
			if *fieldValue.Value.String != string(v1.ConditionFalse) {
				continue
			}
			msg := strings.TrimSpace(GetConditionMessageFromManifestWorkStatus(
				&ranv1alpha1.ManifestWorkStatus{Status: mw.Status.ResourceStatus}))
			if msg == "" {
				msg = fmt.Sprintf("LCA reported stage %s failed",
					strings.TrimSuffix(string(ct), "Completed"))
			}
			return true, msg
		}
	}
	return false, ""
}

// GetManifestWorkForCluster returns the manifest work instance for the given spoke
func GetManifestWorkForCluster(ctx context.Context, client client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	startIndex int, clusterName string) (*mwv1.ManifestWork, error) {
	name := getManifestWorkName(clusterGroupUpgrade, startIndex)
	mw := &mwv1.ManifestWork{}
	err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: clusterName}, mw)
	return mw, err
}

// GetManifestsFromTemplate returns the list of manifests from the manifestwork template
func GetManifestsFromTemplate(ctx context.Context, client client.Client, name types.NamespacedName) (manifests []mwv1.Manifest, err error) {
	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{}
	err = client.Get(ctx, name, mwrs)
	if err != nil {
		return nil, err
	}
	return mwrs.Spec.ManifestWorkTemplate.Workload.Manifests, nil
}

// CreateManifestWorkForCluster creates the manifest work instance for the given spoke
func CreateManifestWorkForCluster(ctx context.Context, client client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	index int, clusterName string) error {
	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{}
	err := client.Get(ctx, types.NamespacedName{Name: clusterGroupUpgrade.Spec.ManifestWorkTemplates[index], Namespace: clusterGroupUpgrade.Namespace}, mwrs)
	if err != nil {
		return err
	}

	name := getManifestWorkName(clusterGroupUpgrade, index)
	mw := &mwv1.ManifestWork{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: clusterName,
			Labels: map[string]string{
				"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
				"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace,
			},
			Annotations: map[string]string{
				manifestWorkExpectedValuesAnnotation: mwrs.Annotations[manifestWorkExpectedValuesAnnotation],
			},
		},
		Spec: mwrs.Spec.ManifestWorkTemplate,
	}
	return client.Create(ctx, mw)
}

// CleanupManifestWorkForBatch deletes manifestwork instances for all clusters in the given batch
func CleanupManifestWorkForBatch(ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batchIndex int) error {
	if clusterGroupUpgrade.RolloutType() != ranv1alpha1.RolloutTypes.ManifestWork {
		return nil
	}
	var labels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace}
	for _, clusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {

		deleteAllOpts := []client.DeleteAllOfOption{
			client.InNamespace(clusterName),
			client.MatchingLabels(labels),
		}

		if err := c.DeleteAllOf(ctx, &mwv1.ManifestWork{}, deleteAllOpts...); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete manifestwork for cluster %s due to err %v", clusterName, err)
		}
	}
	return nil
}
