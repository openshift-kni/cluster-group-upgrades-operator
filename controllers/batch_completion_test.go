package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mwv1 "open-cluster-management.io/api/work/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	testscheme.AddKnownTypes(viewv1beta1.GroupVersion, &viewv1beta1.ManagedClusterView{}, &viewv1beta1.ManagedClusterViewList{})
	testscheme.AddKnownTypes(mwv1.SchemeGroupVersion, &mwv1.ManifestWork{}, &mwv1.ManifestWorkList{})
}

const batchTestNamespace = "ztp-group"

func newBatchReconciler(t *testing.T, objs ...client.Object) *ClusterGroupUpgradeReconciler {
	t.Helper()
	c, err := getFakeClientFromObjects(objs...)
	require.NoError(t, err)
	return &ClusterGroupUpgradeReconciler{
		Client: c, Log: logr.Discard(), Scheme: testscheme,
		EventEmitter: NewEmitter(c, testscheme, "ClusterGroupUpgrade"),
	}
}

// newBatchCGU returns a CGU on its last batch. Batch 1 is spoke1, batch 2 is spoke2.
func newBatchCGU(manifestWork bool) *ranv1alpha1.ClusterGroupUpgrade {
	enable := true
	cgu := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cgu", Namespace: batchTestNamespace,
			Annotations: map[string]string{utils.NameSuffixAnnotation: "abcde"},
		},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{Enable: &enable},
		Status: ranv1alpha1.ClusterGroupUpgradeStatus{
			RemediationPlan: [][]string{{"spoke1"}, {"spoke2"}},
			Status: ranv1alpha1.UpgradeStatus{
				CurrentBatch: 2,
				CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
					"spoke2": {State: ranv1alpha1.NotStarted},
				},
			},
		},
	}
	if manifestWork {
		cgu.Spec.ManifestWorkTemplates = []string{"ibgu-prep"}
	} else {
		cgu.Status.ManagedPoliciesForUpgrade = []ranv1alpha1.ManagedPolicyForUpgrade{{Name: "common-config", Namespace: batchTestNamespace}}
	}
	return cgu
}

// appliedManifestWork returns the ManifestWork for the CGU's only template on cluster.
func appliedManifestWork(cgu *ranv1alpha1.ClusterGroupUpgrade, cluster string, ready bool) *mwv1.ManifestWork {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	return &mwv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GetSafeResourceName(cgu.Namespace+"."+cgu.Spec.ManifestWorkTemplates[0], "", cgu, utils.MaxObjectNameLength),
			Namespace: cluster,
		},
		Status: mwv1.ManifestWorkStatus{Conditions: []metav1.Condition{
			{Type: mwv1.WorkApplied, Status: status},
			{Type: mwv1.WorkAvailable, Status: status},
		}},
	}
}

// parentPolicy returns the CGU's managed policy with the given per-cluster compliance.
func parentPolicy(compliance map[string]policiesv1.ComplianceState) *policiesv1.Policy {
	p := &policiesv1.Policy{ObjectMeta: metav1.ObjectMeta{Name: "common-config", Namespace: batchTestNamespace}}
	for cluster, state := range compliance {
		p.Status.Status = append(p.Status.Status, &policiesv1.CompliancePerClusterStatus{
			ClusterName: cluster, ClusterNamespace: cluster, ComplianceState: state,
		})
	}
	return p
}

func TestIsUpgradeComplete(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name         string
		manifestWork bool
		objs         func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object
		completed    bool // spoke2 already recorded as Completed
		wantComplete bool
		wantErr      bool
	}{
		{
			name:         "manifestwork: last batch done completes, earlier batches are not re-checked",
			manifestWork: true,
			objs: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				// spoke1 (batch 1) has no ManifestWork at all.
				return []client.Object{appliedManifestWork(cgu, "spoke2", true)}
			},
			wantComplete: true,
		},
		{
			name:         "manifestwork: last batch still applying",
			manifestWork: true,
			objs: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{appliedManifestWork(cgu, "spoke2", false)}
			},
		},
		{
			name:         "manifestwork: cluster already completed is not looked up again",
			manifestWork: true,
			objs:         func(*ranv1alpha1.ClusterGroupUpgrade) []client.Object { return nil },
			completed:    true,
			wantComplete: true,
		},
		{
			name:    "policy: missing managed policy is an error",
			objs:    func(*ranv1alpha1.ClusterGroupUpgrade) []client.Object { return nil },
			wantErr: true,
		},
		{
			name: "policy: every cluster compliant",
			objs: func(*ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{parentPolicy(map[string]policiesv1.ComplianceState{
					"spoke1": policiesv1.Compliant, "spoke2": policiesv1.Compliant,
				})}
			},
			wantComplete: true,
		},
		{
			name: "policy: earlier batch cluster drifted to non-compliant",
			objs: func(*ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{parentPolicy(map[string]policiesv1.ComplianceState{
					"spoke1": policiesv1.NonCompliant, "spoke2": policiesv1.Compliant,
				})}
			},
		},
		{
			name: "policy: last batch cluster still non-compliant",
			objs: func(*ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{parentPolicy(map[string]policiesv1.ComplianceState{
					"spoke1": policiesv1.Compliant, "spoke2": policiesv1.NonCompliant,
				})}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newBatchCGU(tc.manifestWork)
			if tc.completed {
				cgu.Status.Status.CurrentBatchRemediationProgress["spoke2"].State = ranv1alpha1.Completed
			}
			r := newBatchReconciler(t, tc.objs(cgu)...)

			complete, _, _, err := r.isUpgradeComplete(ctx, cgu)
			if tc.wantErr {
				assert.Error(t, err)
				assert.False(t, complete)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantComplete, complete)

			progress := cgu.Status.Status.CurrentBatchRemediationProgress["spoke2"]
			switch {
			case tc.completed:
				// Already recorded earlier: no new status.clusters entry.
				assert.Empty(t, cgu.Status.Clusters)
			case progress.State == ranv1alpha1.Completed:
				assert.Equal(t, []ranv1alpha1.ClusterState{{Name: "spoke2", State: utils.ClusterRemediationComplete}}, cgu.Status.Clusters)
				assert.Nil(t, progress.PolicyIndex)
				assert.Nil(t, progress.ManifestWorkIndex)
			default:
				assert.Equal(t, ranv1alpha1.InProgress, progress.State)
				assert.Empty(t, cgu.Status.Clusters)
			}
		})
	}
}

func TestArePreviousBatchesCompleteForPolicies(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name         string
		plan         [][]string
		compliance   map[string]policiesv1.ComplianceState
		noPolicy     bool
		wantComplete bool
		wantErr      bool
	}{
		{
			name:         "single batch has no previous batches",
			plan:         [][]string{{"spoke1"}},
			compliance:   map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant},
			wantComplete: true,
		},
		{
			name: "all previous clusters compliant, last batch ignored",
			plan: [][]string{{"spoke1", "spoke2"}, {"spoke3"}},
			compliance: map[string]policiesv1.ComplianceState{
				"spoke1": policiesv1.Compliant, "spoke2": policiesv1.Compliant, "spoke3": policiesv1.NonCompliant,
			},
			wantComplete: true,
		},
		{
			name: "a previous cluster is non-compliant",
			plan: [][]string{{"spoke1"}, {"spoke2"}, {"spoke3"}},
			compliance: map[string]policiesv1.ComplianceState{
				"spoke1": policiesv1.Compliant, "spoke2": policiesv1.NonCompliant, "spoke3": policiesv1.Compliant,
			},
		},
		{
			name:       "a previous cluster with no compliance entry counts as done",
			plan:       [][]string{{"spoke1"}, {"spoke2"}},
			compliance: map[string]policiesv1.ComplianceState{"spoke2": policiesv1.Compliant},
			// spoke1 is not matched by the policy, so the policy does not apply to it.
			wantComplete: true,
		},
		{
			name:     "policy lookup error",
			plan:     [][]string{{"spoke1"}, {"spoke2"}},
			noPolicy: true,
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newBatchCGU(false)
			cgu.Status.RemediationPlan = tc.plan
			var objs []client.Object
			if !tc.noPolicy {
				objs = append(objs, parentPolicy(tc.compliance))
			}
			r := newBatchReconciler(t, objs...)

			complete, soaking, err := r.arePreviousBatchesCompleteForPolicies(ctx, cgu)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.wantComplete, complete)
			assert.False(t, soaking)
		})
	}
}

func TestSetCompletionConditions(t *testing.T) {
	completed := ranv1alpha1.ClusterState{Name: "spoke1", State: utils.ClusterRemediationComplete}
	timedOut := ranv1alpha1.ClusterState{Name: "spoke2", State: utils.ClusterRemediationTimedout}

	tests := []struct {
		name          string
		manifestWork  bool
		clusters      []ranv1alpha1.ClusterState
		wantSucceeded metav1.ConditionStatus
		wantReason    utils.ConditionReason
		wantMessage   string
	}{
		{
			name:          "manifestwork: every cluster completed",
			manifestWork:  true,
			clusters:      []ranv1alpha1.ClusterState{completed},
			wantSucceeded: metav1.ConditionTrue,
			wantReason:    utils.ConditionReasons.Completed,
			wantMessage:   utils.CompletedMessages[ranv1alpha1.RolloutTypes.ManifestWork],
		},
		{
			name:          "manifestwork: an earlier batch timed out",
			manifestWork:  true,
			clusters:      []ranv1alpha1.ClusterState{timedOut, completed},
			wantSucceeded: metav1.ConditionFalse,
			wantReason:    utils.ConditionReasons.TimedOut,
			wantMessage:   utils.TimeoutMessages[ranv1alpha1.RolloutTypes.ManifestWork] + " on some clusters",
		},
		{
			// Policy rollouts re-check earlier batches before completing, so they keep the existing outcome.
			name:          "policy: unchanged",
			clusters:      []ranv1alpha1.ClusterState{timedOut, completed},
			wantSucceeded: metav1.ConditionTrue,
			wantReason:    utils.ConditionReasons.Completed,
			wantMessage:   utils.CompletedMessages[ranv1alpha1.RolloutTypes.Policy],
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newBatchCGU(tc.manifestWork)
			cgu.Status.Clusters = tc.clusters

			setCompletionConditions(cgu)

			progressing := meta.FindStatusCondition(cgu.Status.Conditions, string(utils.ConditionTypes.Progressing))
			succeeded := meta.FindStatusCondition(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded))
			require.NotNil(t, progressing)
			require.NotNil(t, succeeded)
			assert.Equal(t, metav1.ConditionFalse, progressing.Status)
			assert.Equal(t, string(tc.wantReason), progressing.Reason)
			assert.Equal(t, tc.wantSucceeded, succeeded.Status)
			assert.Equal(t, string(tc.wantReason), succeeded.Reason)
			assert.Equal(t, tc.wantMessage, succeeded.Message)
		})
	}
}

// reconcileLastBatch is the path Reconcile takes on the last batch: it must end
// a ManifestWork CGU as TimedOut when an earlier batch timed out.
func TestReconcileLastBatch(t *testing.T) {
	ctx := context.Background()
	timedOut := ranv1alpha1.ClusterState{Name: "spoke1", State: utils.ClusterRemediationTimedout}
	completed := ranv1alpha1.ClusterState{Name: "spoke1", State: utils.ClusterRemediationComplete}

	tests := []struct {
		name          string
		earlier       ranv1alpha1.ClusterState
		lastBatchDone bool
		wantSucceeded *metav1.ConditionStatus
		wantReason    utils.ConditionReason
	}{
		{"earlier batch timed out", timedOut, true, ptrTo(metav1.ConditionFalse), utils.ConditionReasons.TimedOut},
		{"every cluster completed", completed, true, ptrTo(metav1.ConditionTrue), utils.ConditionReasons.Completed},
		{"last batch still running", timedOut, false, nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newBatchCGU(true)
			cgu.Status.Clusters = []ranv1alpha1.ClusterState{tc.earlier}
			r := newBatchReconciler(t, appliedManifestWork(cgu, "spoke2", tc.lastBatchDone))

			var next ctrl.Result
			require.NoError(t, r.reconcileLastBatch(ctx, cgu, &next))

			succeeded := meta.FindStatusCondition(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded))
			if tc.wantSucceeded == nil {
				assert.Nil(t, succeeded)
				return
			}
			require.NotNil(t, succeeded)
			assert.Equal(t, *tc.wantSucceeded, succeeded.Status)
			assert.Equal(t, string(tc.wantReason), succeeded.Reason)
			assert.Equal(t, requeueImmediately(), next)
		})
	}
}

func TestReconcileLastBatchError(t *testing.T) {
	// A policy CGU whose managed policy is missing cannot evaluate the last batch.
	cgu := newBatchCGU(false)
	r := newBatchReconciler(t)
	var next ctrl.Result
	assert.Error(t, r.reconcileLastBatch(context.Background(), cgu, &next))
	assert.Empty(t, cgu.Status.Conditions)
}

// A CGU that lists a ManifestWork CGU as a blocking CR keeps waiting when that
// CGU finished TimedOut (Succeeded=False), unless it opts into partial mode.
func TestBlockingCRsNotCompletedWithTimedOutBlockingCGU(t *testing.T) {
	ctx := context.Background()
	blocking := func(succeeded metav1.ConditionStatus) *ranv1alpha1.ClusterGroupUpgrade {
		cgu := newBatchCGU(true)
		cgu.Name = "blocking"
		cgu.Status.Conditions = []metav1.Condition{{
			Type: string(utils.ConditionTypes.Succeeded), Status: succeeded, Reason: string(utils.ConditionReasons.TimedOut),
		}}
		return cgu
	}
	dependent := func(partial bool) *ranv1alpha1.ClusterGroupUpgrade {
		cgu := newBatchCGU(true)
		cgu.Name = "dependent"
		cgu.Spec.BlockingCRs = []ranv1alpha1.BlockingCR{{Name: "blocking", Namespace: batchTestNamespace}}
		if partial {
			cgu.Annotations[utils.BlockingCGUCompletionModeAnn] = utils.PartialBlockingCGUCompletion
		}
		return cgu
	}

	tests := []struct {
		name        string
		succeeded   metav1.ConditionStatus
		partial     bool
		wantBlocked bool
	}{
		{"timed out blocks in default mode", metav1.ConditionFalse, false, true},
		{"timed out does not block in partial mode", metav1.ConditionFalse, true, false},
		{"succeeded does not block", metav1.ConditionTrue, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newBatchReconciler(t, blocking(tc.succeeded))
			notCompleted, missing, err := r.blockingCRsNotCompleted(ctx, dependent(tc.partial))
			require.NoError(t, err)
			assert.Empty(t, missing)
			if tc.wantBlocked {
				assert.Equal(t, []string{"blocking"}, notCompleted)
			} else {
				assert.Empty(t, notCompleted)
			}
		})
	}
}

func ptrTo[T any](v T) *T { return &v }
