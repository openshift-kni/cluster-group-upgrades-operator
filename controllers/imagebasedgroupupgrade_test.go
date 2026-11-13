package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcabcv1 "github.com/openshift-kni/lifecycle-agent/api/backupcontent/v1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

func init() {
	testscheme.AddKnownTypes(ibguv1alpha1.SchemeGroupVersion, &ibguv1alpha1.ImageBasedGroupUpgrade{})
	testscheme.AddKnownTypes(ibguv1alpha1.SchemeGroupVersion,
		&ibguv1alpha1.BackupContent{}, &ibguv1alpha1.BackupContentList{})
	testscheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ClusterGroupUpgradeList{})
	testscheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ClusterGroupUpgrade{})
	testscheme.AddKnownTypes(mwv1alpha1.SchemeGroupVersion, &mwv1alpha1.ManifestWorkReplicaSet{})
	testscheme.AddKnownTypes(mwv1alpha1.SchemeGroupVersion, &mwv1alpha1.ManifestWorkReplicaSetList{})
	testscheme.AddKnownTypes(mwv1.SchemeGroupVersion, &mwv1.ManifestWork{}, &mwv1.ManifestWorkList{})
	testscheme.AddKnownTypes(clusterv1.SchemeGroupVersion, &clusterv1.ManagedClusterList{})
}

func TestSyncStatusWithCGUs(t *testing.T) {
	errorMsg := "error message"
	cmw := &v1alpha1.ManifestWorkStatus{
		Name: "ibu-finalizeupgrade",
		Status: mwv1.ManifestResourceStatus{
			Manifests: []mwv1.ManifestCondition{
				{
					StatusFeedbacks: mwv1.StatusFeedbackResult{
						Values: []mwv1.FeedbackValue{
							{
								Name: "idleCompletedConditionMessages",
								Value: mwv1.FieldValue{
									Type:   mwv1.String,
									String: &errorMsg,
								},
							},
						},
					},
				},
			},
		},
	}

	two := 2
	falseStr := "False"
	lcaFailureMsg := "LCA: upgrade failed on spoke9"
	tests := []struct {
		name                   string
		CGUs                   []v1alpha1.ClusterGroupUpgrade
		manifestWorks          []mwv1.ManifestWork
		expectedClustersStatus []ibguv1alpha1.ClusterState
	}{
		{
			name: "no CGUs",
		},
		{
			name: "two CGUs",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-upgrade",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-prep", "name-upgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Clusters: []v1alpha1.ClusterState{
							{
								Name:  "spoke1",
								State: "complete",
							},
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-finalizeupgrade",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-finalizeupgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Clusters: []v1alpha1.ClusterState{
							{
								Name:                "spoke1",
								State:               "timedout",
								CurrentManifestWork: cmw,
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name: "spoke1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade},
					},
					FailedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.FinalizeUpgrade, Message: "error message"},
					},
				},
			},
		},
		{
			name: "two CGUs with reverse order",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-finalizeupgrade-1",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-finalizeupgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Clusters: []v1alpha1.ClusterState{
							{
								Name:  "spoke1",
								State: "complete",
							},
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-upgrade-0	",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-prep", "name-upgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Clusters: []v1alpha1.ClusterState{
							{
								Name:  "spoke1",
								State: "complete",
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name: "spoke1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade}, {Action: ibguv1alpha1.FinalizeUpgrade},
					},
				},
			},
		},
		{
			name: "two CGUs one in progress",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-upgrade",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-prep", "name-upgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Clusters: []v1alpha1.ClusterState{
							{
								Name:  "spoke1",
								State: "complete",
							},
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-finalizeupgrade",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-finalizeupgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Status: v1alpha1.UpgradeStatus{
							CurrentBatchRemediationProgress: map[string]*v1alpha1.ClusterRemediationProgress{
								"spoke1": {
									ManifestWorkIndex: new(int),
									State:             "InProgress",
								},
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name: "spoke1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade},
					},
					CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.FinalizeUpgrade},
				},
			},
		},
		{
			name: "one cgu in progress",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-prep"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Status: v1alpha1.UpgradeStatus{
							CurrentBatchRemediationProgress: map[string]*v1alpha1.ClusterRemediationProgress{
								"spoke6": {
									ManifestWorkIndex: new(int),
									State:             "InProgress",
								},
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name:          "spoke6",
					CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.Prep},
				},
			},
		},
		{
			name: "one cgu",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-upgrade-finalizeupgrade",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-prep", "name-upgrade", "name-finalizeupgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Clusters: []v1alpha1.ClusterState{
							{
								Name:  "spoke1",
								State: "complete",
							},
							{
								Name:  "spoke4",
								State: "complete",
							},
						},
						Status: v1alpha1.UpgradeStatus{
							CurrentBatchRemediationProgress: map[string]*v1alpha1.ClusterRemediationProgress{
								"spoke6": {
									ManifestWorkIndex: &two,
									State:             "InProgress",
								},
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name: "spoke1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade}, {Action: ibguv1alpha1.FinalizeUpgrade},
					},
				},
				{
					Name: "spoke4",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade}, {Action: ibguv1alpha1.FinalizeUpgrade},
					},
				},
				{
					Name:          "spoke6",
					CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.FinalizeUpgrade},
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade},
					},
				},
			},
		},
		{
			name: "in progress cluster with LCA-reported stage failure",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-upgrade",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-upgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						// Pin the generated ManifestWork name so the report-only path reads
						// the ManifestWork created below instead of a random-suffixed name.
						SafeResourceNames: map[string]string{
							"/namespace.name-upgrade": "mw-spoke9-upgrade",
						},
						Status: v1alpha1.UpgradeStatus{
							CurrentBatchRemediationProgress: map[string]*v1alpha1.ClusterRemediationProgress{
								"spoke9": {
									ManifestWorkIndex: new(int),
									State:             "InProgress",
								},
							},
						},
					},
				},
			},
			manifestWorks: []mwv1.ManifestWork{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "mw-spoke9-upgrade",
						Namespace: "spoke9",
					},
					Status: mwv1.ManifestWorkStatus{
						ResourceStatus: mwv1.ManifestResourceStatus{
							Manifests: []mwv1.ManifestCondition{
								{
									ResourceMeta: mwv1.ManifestResourceMeta{Ordinal: 0},
									Conditions: []v1.Condition{
										{Type: mwv1.ManifestApplied, Status: v1.ConditionTrue},
										{Type: mwv1.ManifestAvailable, Status: v1.ConditionTrue},
										{Type: "StatusFeedbackSynced", Status: v1.ConditionTrue},
									},
									StatusFeedbacks: mwv1.StatusFeedbackResult{
										Values: []mwv1.FeedbackValue{
											{
												Name:  "isUpgradeCompleted",
												Value: mwv1.FieldValue{Type: mwv1.String, String: &falseStr},
											},
											{
												Name:  "upgradeCompletedConditionMessages",
												Value: mwv1.FieldValue{Type: mwv1.String, String: &lcaFailureMsg},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name:          "spoke9",
					CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.Upgrade},
					FailedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Upgrade, Message: "LCA: upgrade failed on spoke9"},
					},
				},
			},
		},
		{
			name: "in progress cluster with no stage failure reported",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-upgrade",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-upgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						SafeResourceNames: map[string]string{
							"/namespace.name-upgrade": "mw-spoke9-upgrade",
						},
						Status: v1alpha1.UpgradeStatus{
							CurrentBatchRemediationProgress: map[string]*v1alpha1.ClusterRemediationProgress{
								"spoke9": {
									ManifestWorkIndex: new(int),
									State:             "InProgress",
								},
							},
						},
					},
				},
			},
			manifestWorks: []mwv1.ManifestWork{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "mw-spoke9-upgrade",
						Namespace: "spoke9",
					},
					Status: mwv1.ManifestWorkStatus{
						ResourceStatus: mwv1.ManifestResourceStatus{
							Manifests: []mwv1.ManifestCondition{
								{
									ResourceMeta: mwv1.ManifestResourceMeta{Ordinal: 0},
									Conditions: []v1.Condition{
										{Type: mwv1.ManifestApplied, Status: v1.ConditionTrue},
										{Type: mwv1.ManifestAvailable, Status: v1.ConditionTrue},
										{Type: "StatusFeedbackSynced", Status: v1.ConditionTrue},
									},
									StatusFeedbacks: mwv1.StatusFeedbackResult{
										Values: []mwv1.FeedbackValue{
											{
												Name:  "upgradeInProgressConditionMessage",
												Value: mwv1.FieldValue{Type: mwv1.String, String: &lcaFailureMsg},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name:          "spoke9",
					CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.Upgrade},
				},
			},
		},
	}
	ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{

			ClusterLabelSelectors: []v1.LabelSelector{

				{
					MatchLabels: map[string]string{
						"common": "true",
					},
				},
			},
			IBUSpec: lcav1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1.SeedImageRef{
					Version: "version",
					Image:   "image",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			for _, cgu := range test.CGUs {
				err := fakeClient.Create(context.TODO(), &cgu)
				if err != nil {
					panic(err)
				}
			}
			for _, mw := range test.manifestWorks {
				mw := mw
				if err := fakeClient.Create(context.TODO(), &mw); err != nil {
					panic(err)
				}
			}
			if err != nil {
				t.Errorf("error in creating fake client")
			}
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			err = reconciler.syncStatusWithCGUs(context.Background(), ibgu)
			assert.NoError(t, err)
			assert.ElementsMatch(t, test.expectedClustersStatus, ibgu.Status.Clusters)
		})
	}
}

func TestCountInProgressStageFailures(t *testing.T) {
	tests := []struct {
		name string
		ibgu *ibguv1alpha1.ImageBasedGroupUpgrade
		want int
	}{
		{
			name: "no clusters",
			ibgu: &ibguv1alpha1.ImageBasedGroupUpgrade{},
			want: 0,
		},
		{
			name: "in-progress cluster with a reported stage failure is counted",
			ibgu: &ibguv1alpha1.ImageBasedGroupUpgrade{
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Clusters: []ibguv1alpha1.ClusterState{
						{
							Name:          "spoke1",
							CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.Upgrade},
							FailedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Upgrade, Message: "boom"}},
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "in-progress cluster whose failure is from a different action is not counted",
			ibgu: &ibguv1alpha1.ImageBasedGroupUpgrade{
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Clusters: []ibguv1alpha1.ClusterState{
						{
							Name:          "spoke1",
							CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.AbortOnFailure},
							FailedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Upgrade, Message: "timed out"}},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "terminal failed cluster without a current action is not counted",
			ibgu: &ibguv1alpha1.ImageBasedGroupUpgrade{
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Clusters: []ibguv1alpha1.ClusterState{
						{
							Name:          "spoke1",
							FailedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Upgrade, Message: "timed out"}},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "in-progress cluster with no failure is not counted",
			ibgu: &ibguv1alpha1.ImageBasedGroupUpgrade{
				Status: ibguv1alpha1.ImageBasedGroupUpgradeStatus{
					Clusters: []ibguv1alpha1.ClusterState{
						{
							Name:          "spoke1",
							CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.Upgrade},
						},
					},
				},
			},
			want: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, countInProgressStageFailures(test.ibgu))
		})
	}
}

func TestGetSecretManifest(t *testing.T) {
	tests := []struct {
		name         string
		expectedJson string
		secret       corev1.Secret
	}{
		{
			name: "simple secret",
			secret: corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      "secret",
					Namespace: "namespace",
				},
			},
			expectedJson: `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"secret","namespace":"openshift-lifecycle-agent"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pull := lcav1.PullSecretRef{Name: "secret"}
			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef: lcav1.SeedImageRef{
							Version:       "version",
							Image:         "image",
							PullSecretRef: &pull,
						},
					},
				},
			}

			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			if err != nil {
				t.Errorf("error in creating fake client")
			}
			err = fakeClient.Create(context.TODO(), &test.secret)
			if err != nil {
				panic(err)
			}
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			manifest, err := reconciler.getSecretManifest(context.TODO(), ibgu)
			assert.NoError(t, err)
			assert.JSONEq(t, test.expectedJson, string(manifest.Raw))
		})
	}
}

// TestGetSecretManifestErrors verifies that getSecretManifest no longer silently
// drops the pull secret: with no pullSecretRef it returns (nil, nil), but a read
// failure (the reference gate has already established the secret exists) is returned
// as an error rather than producing a Prep CGU without the credentials a private seed
// image needs.
func TestGetSecretManifestErrors(t *testing.T) {
	newIBGU := func(pullSecretRef *lcav1.PullSecretRef) *ibguv1alpha1.ImageBasedGroupUpgrade {
		return &ibguv1alpha1.ImageBasedGroupUpgrade{
			ObjectMeta: v1.ObjectMeta{Name: "name", Namespace: "namespace"},
			Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
				IBUSpec: lcav1.ImageBasedUpgradeSpec{
					SeedImageRef: lcav1.SeedImageRef{
						Version: "version", Image: "image", PullSecretRef: pullSecretRef,
					},
				},
			},
		}
	}

	t.Run("no pullSecretRef returns no manifest and no error", func(t *testing.T) {
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		manifest, err := reconciler.getSecretManifest(context.TODO(), newIBGU(nil))
		assert.NoError(t, err)
		assert.Nil(t, manifest)
	})

	t.Run("a NotFound read is returned as an error (the gate established it exists)", func(t *testing.T) {
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		manifest, err := reconciler.getSecretManifest(context.TODO(), newIBGU(&lcav1.PullSecretRef{Name: "missing"}))
		assert.Error(t, err)
		assert.Nil(t, manifest)
	})

	t.Run("a transient read failure is returned as an error", func(t *testing.T) {
		base, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		faultyClient := interceptor.NewClient(base, interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					return apierrors.NewInternalError(fmt.Errorf("etcd unavailable"))
				}
				return nil
			},
		})
		reconciler := IBGUReconciler{Client: faultyClient, APIReader: faultyClient, Scheme: testscheme, Log: logr.Discard()}
		manifest, err := reconciler.getSecretManifest(context.TODO(), newIBGU(&lcav1.PullSecretRef{Name: "pull"}))
		assert.Error(t, err)
		assert.Nil(t, manifest)
	})
}

func TestCreateIBU(t *testing.T) {
	tests := []struct {
		name    string
		ibuSpec lcav1.ImageBasedUpgradeSpec
	}{
		{
			name: "backupContent is copied through",
			ibuSpec: lcav1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1.SeedImageRef{Version: "version", Image: "image"},
				BackupContent: []lcav1.BackupContentRef{
					{Name: "bc1"},
					{Name: "bc2"},
				},
			},
		},
		{
			name: "oadpContent is copied through (existing behavior)",
			ibuSpec: lcav1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1.SeedImageRef{Version: "version", Image: "image"},
				OADPContent: []lcav1.ConfigMapRef{
					{Name: "cm1", Namespace: "ns1"},
				},
			},
		},
		{
			name: "neither oadpContent nor backupContent",
			ibuSpec: lcav1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1.SeedImageRef{Version: "version", Image: "image"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name:        "name",
					Namespace:   "namespace",
					Annotations: map[string]string{LcaAnnotationSuffix: "value"},
				},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: test.ibuSpec,
				},
			}

			ibu := createIBU(ibgu)

			// createIBU is a pure copy of ibuSpec: every field, including the
			// backupContent reference list introduced by the LCA revendor, must
			// flow through unchanged.
			assert.Equal(t, "upgrade", ibu.Name)
			assert.Equal(t, test.ibuSpec, ibu.Spec)
			assert.Equal(t, test.ibuSpec.BackupContent, ibu.Spec.BackupContent)
			assert.Equal(t, test.ibuSpec.OADPContent, ibu.Spec.OADPContent)
		})
	}
}

func TestGetConfigMapManifests(t *testing.T) {
	tests := []struct {
		name         string
		cm           corev1.ConfigMap
		createCM     bool
		expectedJson string
		expectErr    bool
	}{
		{
			name: "simple configmap",
			cm: corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
				Data: map[string]string{
					"klusterlet.yaml": "data",
				},
			},
			createCM:     true,
			expectedJson: `{"kind":"ConfigMap","apiVersion":"v1","metadata":{"name":"name","namespace":"namespace"},"data":{"klusterlet.yaml":"data"}}`,
		},
		{
			name: "missing configmap returns error",
			cm: corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
			},
			createCM:  false,
			expectErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef: lcav1.SeedImageRef{
							Version: "version",
							Image:   "image",
						},
						OADPContent: []lcav1.ConfigMapRef{
							{
								Name:      test.cm.Name,
								Namespace: test.cm.Namespace,
							},
						},
					},
				},
			}

			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			if err != nil {
				t.Errorf("error in creating fake client")
			}
			if test.createCM {
				err = fakeClient.Create(context.TODO(), &test.cm)
				if err != nil {
					panic(err)
				}
			}
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			manifests, err := reconciler.getConfigMapManifests(context.TODO(), ibgu)
			if test.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, 1, len(manifests))
			assert.JSONEq(t, test.expectedJson, string(manifests[0].Raw))
		})
	}
}

func TestGetBackupContentManifests(t *testing.T) {
	backupWave := int32(2)
	restoreWave := int32(3)
	spec := lcabcv1.BackupContentSpec{
		BackupWave:  &backupWave,
		RestoreWave: &restoreWave,
		Namespaces:  []string{"open-cluster-management-agent"},
		NamespacedResources: []lcabcv1.ResourceSelector{
			{Kind: "deployments.apps", Names: []string{"klusterlet"}},
		},
	}
	// A fresh spoke BackupContent: apiVersion rewritten to lca.openshift.io/v1, name
	// verbatim, namespace openshift-lifecycle-agent, hub spec verbatim, and none of the
	// hub metadata (resourceVersion/managedFields/creationTimestamp/uid/ownerReferences/
	// generation/labels) carried over.
	expectedJSON := `{"apiVersion":"lca.openshift.io/v1","kind":"BackupContent",` +
		`"metadata":{"name":"platform","namespace":"openshift-lifecycle-agent"},` +
		`"spec":{"backupWave":2,"namespacedResources":[{"kind":"deployments.apps","names":["klusterlet"]}],` +
		`"namespaces":["open-cluster-management-agent"],"restoreWave":3},"status":{}}`

	tests := []struct {
		name          string
		refs          []string
		createdHubBCs []*ibguv1alpha1.BackupContent
		expectErr     bool
		expectedJSON  []string
	}{
		{
			name: "single reference copied to the spoke",
			refs: []string{"platform"},
			createdHubBCs: []*ibguv1alpha1.BackupContent{
				{
					// The fake client assigns a resourceVersion/uid on create; together
					// with the label here these exercise that the spoke copy carries none
					// of the hub metadata.
					ObjectMeta: v1.ObjectMeta{
						Name:      "platform",
						Namespace: "namespace",
						Labels:    map[string]string{"hub-label": "value"},
					},
					Spec: spec,
				},
			},
			expectedJSON: []string{expectedJSON},
		},
		{
			name:      "missing hub BackupContent returns error",
			refs:      []string{"platform"},
			expectErr: true,
		},
		{
			name: "multiple references copied in order",
			refs: []string{"platform", "apps"},
			createdHubBCs: []*ibguv1alpha1.BackupContent{
				{
					ObjectMeta: v1.ObjectMeta{Name: "platform", Namespace: "namespace"},
					Spec:       spec,
				},
				{
					ObjectMeta: v1.ObjectMeta{Name: "apps", Namespace: "namespace"},
					Spec:       spec,
				},
			},
			expectedJSON: []string{
				expectedJSON,
				`{"apiVersion":"lca.openshift.io/v1","kind":"BackupContent",` +
					`"metadata":{"name":"apps","namespace":"openshift-lifecycle-agent"},` +
					`"spec":{"backupWave":2,"namespacedResources":[{"kind":"deployments.apps","names":["klusterlet"]}],` +
					`"namespaces":["open-cluster-management-agent"],"restoreWave":3},"status":{}}`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			refs := []lcav1.BackupContentRef{}
			for _, name := range test.refs {
				refs = append(refs, lcav1.BackupContentRef{Name: name})
			}
			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef:  lcav1.SeedImageRef{Version: "version", Image: "image"},
						BackupContent: refs,
					},
				},
			}
			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			if err != nil {
				t.Errorf("error in creating fake client")
			}
			for _, hubBC := range test.createdHubBCs {
				if err := fakeClient.Create(context.TODO(), hubBC); err != nil {
					panic(err)
				}
			}
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			manifests, err := reconciler.getBackupContentManifests(context.TODO(), ibgu)
			if test.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, len(test.expectedJSON), len(manifests))
			for i, expected := range test.expectedJSON {
				assert.JSONEq(t, expected, string(manifests[i].Raw))
			}
		})
	}
}

func TestEnsureClusterLabels(t *testing.T) {
	tests := []struct {
		name                    string
		managedClusters         []clusterv1.ManagedCluster
		expectedManagedClusters []clusterv1.ManagedCluster
		clusterStates           []ibguv1alpha1.ClusterState
		expectedErr             error
	}{
		{
			name: "failed prep, upgrade and abort",
			managedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:   "cluster1",
						Labels: map[string]string{},
					},
				},
			},
			clusterStates: []ibguv1alpha1.ClusterState{
				{
					Name: "cluster1",
					FailedActions: []ibguv1alpha1.ActionMessage{
						{
							Action: ibguv1alpha1.Prep,
						},
						{
							Action: ibguv1alpha1.Upgrade,
						},
						{
							Action: ibguv1alpha1.Abort,
						},
					},
					CompletedActions: []ibguv1alpha1.ActionMessage{},
				},
			},
			expectedManagedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
						Labels: map[string]string{
							"lcm.openshift.io/ibgu-abort-failed":   "",
							"lcm.openshift.io/ibgu-upgrade-failed": "",
							"lcm.openshift.io/ibgu-prep-failed":    "",
						},
					},
				},
			},
		},
		{
			name: "abort completed",
			managedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
						Labels: map[string]string{
							"lcm.openshift.io/ibgu-prep-failed": "",
						},
					},
				},
			},
			clusterStates: []ibguv1alpha1.ClusterState{
				{
					Name: "cluster1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{
							Action: ibguv1alpha1.Abort,
						},
					},
				},
			},
			expectedManagedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
					},
				},
			},
		},
		{
			name: "finalize completed",
			managedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
						Labels: map[string]string{
							"lcm.openshift.io/ibgu-prep-completed":    "",
							"lcm.openshift.io/ibgu-upgrade-completed": "",
						},
					},
				},
			},
			clusterStates: []ibguv1alpha1.ClusterState{
				{
					Name: "cluster1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{
							Action: ibguv1alpha1.Prep,
						},
						{
							Action: ibguv1alpha1.Upgrade,
						},
						{
							Action: ibguv1alpha1.FinalizeUpgrade,
						},
					},
				},
			},
			expectedManagedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
					},
				},
			},
		},
		{
			name: "prep completed",
			managedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
					},
				},
			},
			clusterStates: []ibguv1alpha1.ClusterState{
				{
					Name: "cluster1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{
							Action: ibguv1alpha1.Prep,
						},
					},
				},
			},
			expectedManagedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
						Labels: map[string]string{
							"lcm.openshift.io/ibgu-prep-completed": "",
						},
					},
				},
			},
		},
		{
			name: "two cluster, first throws an error",
			managedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster2",
					},
				},
			},
			clusterStates: []ibguv1alpha1.ClusterState{
				{
					Name: "cluster1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{
							Action: ibguv1alpha1.Prep,
						},
					},
				},
				{
					Name: "cluster2",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{
							Action: ibguv1alpha1.Prep,
						},
					},
				},
			},
			expectedManagedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster2",
						Labels: map[string]string{
							"lcm.openshift.io/ibgu-prep-completed": "",
						},
					},
				},
			},
			expectedErr: fmt.Errorf("failed to ensure cluster labels for %v", []string{"cluster-1"}),
		},
		{
			name: "two cluster, first nothing to do",
			managedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster2",
					},
				},
			},
			clusterStates: []ibguv1alpha1.ClusterState{
				{
					Name: "cluster1",
				},
				{
					Name: "cluster2",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{
							Action: ibguv1alpha1.Prep,
						},
					},
				},
			},
			expectedManagedClusters: []clusterv1.ManagedCluster{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "cluster2",
						Labels: map[string]string{
							"lcm.openshift.io/ibgu-prep-completed": "",
						},
					},
				},
			},
		},
	}

	ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
			ClusterLabelSelectors: []v1.LabelSelector{

				{
					MatchLabels: map[string]string{
						"common": "true",
					},
				},
			},
			IBUSpec: lcav1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1.SeedImageRef{
					Version: "version",
					Image:   "image",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu.Status.Clusters = test.clusterStates
			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			for _, mc := range test.managedClusters {
				err := fakeClient.Create(context.TODO(), &mc)
				if err != nil {
					panic(err)
				}
			}
			if err != nil {
				t.Errorf("error in creating fake client")
			}
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			err = reconciler.ensureClusterLabels(context.TODO(), ibgu)
			if test.expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, test.expectedErr)
			}
			for _, expected := range test.expectedManagedClusters {
				got := &clusterv1.ManagedCluster{}
				err := fakeClient.Get(context.TODO(), types.NamespacedName{Namespace: expected.Namespace, Name: expected.Name}, got)
				assert.NoError(t, err)
				assert.Equal(t, expected.GetLabels(), got.GetLabels())
			}
		})
	}
}

func TestEnsureManifests(t *testing.T) {
	tests := []struct {
		name               string
		plan               []ibguv1alpha1.PlanItem
		expectedMWRS       []string
		expectedCGUCount   int
		CGUs               []v1alpha1.ClusterGroupUpgrade
		managedClusters    []clusterv1.ManagedCluster
		expectedCGUJsons   []string
		expectedConditions []metav1.Condition
	}{
		{
			name: "without append",
			plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{ibguv1alpha1.Prep, ibguv1alpha1.Upgrade, ibguv1alpha1.FinalizeUpgrade},
				},
			},
			managedClusters: []clusterv1.ManagedCluster{
				{ObjectMeta: v1.ObjectMeta{Name: "cluster1", Labels: map[string]string{"common": "true"}}},
				{ObjectMeta: v1.ObjectMeta{Name: "cluster2", Labels: map[string]string{"common": "true"}}},
			},
			expectedMWRS:     []string{"name-prep", "name-upgrade", "name-finalizeupgrade"},
			expectedCGUCount: 1,
			expectedConditions: []metav1.Condition{
				{
					Type:    string(utils.ConditionTypes.Progressing),
					Reason:  string(utils.ConditionReasons.InProgress),
					Message: "Waiting for plan step 0 to be completed",
					Status:  metav1.ConditionTrue,
				},
			},
			expectedCGUJsons: []string{
				`
{
  "apiVersion": "ran.openshift.io/v1alpha1",
  "kind": "ClusterGroupUpgrade",
  "metadata": {
    
    "labels": {
      "ibgu": "name"
    },
    "name": "name-prep-upgrade-finalizeupgrade-0",
    "namespace": "namespace",
    "ownerReferences": [
      {
        "apiVersion": "lcm.openshift.io/v1alpha1",
        "blockOwnerDeletion": true,
        "controller": true,
        "kind": "ImageBasedGroupUpgrade",
        "name": "name",
        "uid": ""
      }
    ],
    "resourceVersion": "1"
  },
  "spec": {
    "actions": {
      "afterCompletion": {
        "removeClusterAnnotations": [
          "import.open-cluster-management.io/disable-auto-import"
        ]
      },
      "beforeEnable": {
        "addClusterAnnotations": {
          "import.open-cluster-management.io/disable-auto-import": "true"
        }
      }
    },
    "clusters": [
      "cluster1",
      "cluster2"
    ],
    "enable": true,
    "manifestWorkTemplates": [
      "name-prep",
      "name-upgrade",
      "name-finalizeupgrade"
    ],
    "preCachingConfigRef": {},
    "remediationStrategy": {
      "maxConcurrency": 0
    }
  },
  "status": {
    "status": {
      "completedAt": null,
      "currentBatchStartedAt": null,
      "startedAt": null
    }
  }
}
                `,
			},
		},
		{
			name: "append abort to prep",
			plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{ibguv1alpha1.Prep},
				},
				{
					Actions: []string{ibguv1alpha1.Abort},
				},
			},
			expectedMWRS:     []string{"name-abort"},
			expectedCGUCount: 2,
			managedClusters: []clusterv1.ManagedCluster{
				{ObjectMeta: v1.ObjectMeta{Name: "cluster1", Labels: map[string]string{"common": "true"}}},
				{ObjectMeta: v1.ObjectMeta{Name: "cluster2", Labels: map[string]string{"common": "true"}}},
			},
			expectedConditions: []metav1.Condition{
				{
					Type:    string(utils.ConditionTypes.Progressing),
					Reason:  string(utils.ConditionReasons.InProgress),
					Message: "Waiting for plan step 1 to be completed",
					Status:  metav1.ConditionTrue,
				},
			},
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-0",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						Clusters: []string{"cluster1", "cluster2"},
						ManifestWorkTemplates: []string{
							"name-prep",
						},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Conditions: []v1.Condition{
							{
								Type: string(utils.ConditionTypes.Succeeded),
							},
						},
					},
				},
			},
			expectedCGUJsons: []string{
				`
{
  "apiVersion": "ran.openshift.io/v1alpha1",
  "kind": "ClusterGroupUpgrade",
  "metadata": {

    "labels": {
      "ibgu": "name"
    },
    "name": "name-abort-1",
    "namespace": "namespace",
    "ownerReferences": [
      {
        "apiVersion": "lcm.openshift.io/v1alpha1",
        "blockOwnerDeletion": true,
        "controller": true,
        "kind": "ImageBasedGroupUpgrade",
        "name": "name",
        "uid": ""
      }
    ],
    "resourceVersion": "1"
  },
  "spec": {
    "actions": {
      "afterCompletion": {},
      "beforeEnable": {}
    },
    "clusters": [
      "cluster1",
      "cluster2"
    ],
    "enable": true,
    "manifestWorkTemplates": [
      "name-abort"
    ],
    "preCachingConfigRef": {},
    "remediationStrategy": {
      "maxConcurrency": 0
    }
  },
  "status": {
    "status": {
      "completedAt": null,
      "currentBatchStartedAt": null,
      "startedAt": null
    }
  }
}
                `,
			},
		},
		{
			// A clean plan completion records Succeeded=True/Completed stamped with the
			// current generation, regardless of the plan tail. It is final for this
			// generation; an appended plan item bumps the generation and resumes it (see
			// TestAppendResumesCompletion), so a non-finalizing tail no longer needs to
			// leave Succeeded unset to stay resumable.
			name: "all completed, clean plan is terminal for its generation",
			plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{ibguv1alpha1.Prep},
				},
			},
			expectedMWRS:     []string{},
			expectedCGUCount: 1,
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-0",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{
							"name-prep",
						},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Conditions: []v1.Condition{
							{
								Type: string(utils.ConditionTypes.Succeeded),
							},
						},
					},
				},
			},
			expectedConditions: []metav1.Condition{
				{
					Type:    string(utils.ConditionTypes.Progressing),
					Reason:  string(utils.ConditionReasons.Completed),
					Message: "All plan steps are completed",
					Status:  metav1.ConditionFalse,
				},
				{
					Type:    string(utils.ConditionTypes.Succeeded),
					Reason:  string(utils.ConditionReasons.Completed),
					Message: "All plan steps are completed",
					Status:  metav1.ConditionTrue,
				},
			},
		},
		{
			// A finalizing plan tail (FinalizeUpgrade) that has run to completion is
			// terminal: both Progressing=Completed and Succeeded=True are recorded.
			name: "all completed, finalizing tail is terminal",
			plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{ibguv1alpha1.Prep, ibguv1alpha1.Upgrade, ibguv1alpha1.FinalizeUpgrade},
				},
			},
			expectedMWRS:     []string{},
			expectedCGUCount: 1,
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-upgrade-finalizeupgrade-0",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{
							"name-prep", "name-upgrade", "name-finalizeupgrade",
						},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Conditions: []v1.Condition{
							{
								Type: string(utils.ConditionTypes.Succeeded),
							},
						},
					},
				},
			},
			expectedConditions: []metav1.Condition{
				{
					Type:    string(utils.ConditionTypes.Progressing),
					Reason:  string(utils.ConditionReasons.Completed),
					Message: "All plan steps are completed",
					Status:  metav1.ConditionFalse,
				},
				{
					Type:    string(utils.ConditionTypes.Succeeded),
					Reason:  string(utils.ConditionReasons.Completed),
					Message: "All plan steps are completed",
					Status:  metav1.ConditionTrue,
				},
			},
		},
		{
			name: "append",
			plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{ibguv1alpha1.Prep},
				},
				{
					Actions: []string{ibguv1alpha1.Upgrade, ibguv1alpha1.FinalizeUpgrade},
				},
			},
			expectedMWRS:     []string{"name-upgrade", "name-finalizeupgrade"},
			expectedCGUCount: 2,
			managedClusters: []clusterv1.ManagedCluster{
				{ObjectMeta: v1.ObjectMeta{Name: "cluster1", Labels: map[string]string{
					"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""}}},
				{ObjectMeta: v1.ObjectMeta{Name: "cluster2", Labels: map[string]string{
					"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""}}},
			},
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-0",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						Clusters: []string{"cluster1", "cluster2"},
						ManifestWorkTemplates: []string{
							"name-prep",
						},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Conditions: []v1.Condition{
							{
								Type: string(utils.ConditionTypes.Succeeded),
							},
						},
					},
				},
			},
			expectedConditions: []metav1.Condition{
				{
					Type:    string(utils.ConditionTypes.Progressing),
					Reason:  string(utils.ConditionReasons.InProgress),
					Message: "Waiting for plan step 1 to be completed",
					Status:  metav1.ConditionTrue,
				},
			},
			expectedCGUJsons: []string{
				`
{
  "apiVersion": "ran.openshift.io/v1alpha1",
  "kind": "ClusterGroupUpgrade",
  "metadata": {

    "labels": {
      "ibgu": "name"
    },
    "name": "name-upgrade-finalizeupgrade-1",
    "namespace": "namespace",
    "ownerReferences": [
      {
        "apiVersion": "lcm.openshift.io/v1alpha1",
        "blockOwnerDeletion": true,
        "controller": true,
        "kind": "ImageBasedGroupUpgrade",
        "name": "name",
        "uid": ""
      }
    ],
    "resourceVersion": "1"
  },
  "spec": {
    "actions": {
      "afterCompletion": {
        "removeClusterAnnotations": [
          "import.open-cluster-management.io/disable-auto-import"
        ]
      },
      "beforeEnable": {
        "addClusterAnnotations": {
          "import.open-cluster-management.io/disable-auto-import": "true"
        }
      }
    },
    "clusters": [
      "cluster1",
      "cluster2"
    ],
    "enable": true,
    "manifestWorkTemplates": [
      "name-upgrade",
      "name-finalizeupgrade"
    ],
    "preCachingConfigRef": {},
    "remediationStrategy": {
      "maxConcurrency": 0
    }
  },
  "status": {
    "status": {
      "completedAt": null,
      "currentBatchStartedAt": null,
      "startedAt": null
    }
  }
}
                `,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
					// A fresh creation time keeps the pre-flight start deadline open so the
					// pass path creates CGUs (the API server always sets this in production).
					CreationTimestamp: metav1.Now(),
				},
				Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{

					ClusterLabelSelectors: []v1.LabelSelector{

						{
							MatchLabels: map[string]string{
								"common": "true",
							},
						},
					},
					Plan: test.plan,
					IBUSpec: lcav1.ImageBasedUpgradeSpec{
						SeedImageRef: lcav1.SeedImageRef{
							Version: "version",
							Image:   "image",
						},
					},
				},
			}

			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			for _, cgu := range test.CGUs {
				err := fakeClient.Create(context.TODO(), &cgu)
				if err != nil {
					panic(err)
				}
			}
			for i := range test.managedClusters {
				if err := fakeClient.Create(context.TODO(), &test.managedClusters[i]); err != nil {
					panic(err)
				}
			}
			if err != nil {
				t.Errorf("error in creating fake client")
			}
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}

			err = reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)
			list := &mwv1alpha1.ManifestWorkReplicaSetList{}
			err = reconciler.List(context.TODO(), list)
			assert.NoError(t, err)
			mwrsNames := []string{}
			for _, mwrs := range list.Items {
				mwrsNames = append(mwrsNames, mwrs.Name)
			}
			// Exact match (not a subset): a completion case with no expected MWRS must
			// create none, so an unexpected ManifestWorkReplicaSet is caught.
			assert.ElementsMatch(t, test.expectedMWRS, mwrsNames)

			// The total CGU count after reconcile is asserted so a completion case
			// creates no unexpected CGU (and a rollout case creates exactly one).
			cguList := &v1alpha1.ClusterGroupUpgradeList{}
			assert.NoError(t, reconciler.List(context.TODO(), cguList))
			assert.Len(t, cguList.Items, test.expectedCGUCount)

			conditions := []metav1.Condition{}
			for _, c := range ibgu.Status.Conditions {
				c.LastTransitionTime = metav1.Time{}
				conditions = append(conditions, c)
			}
			assert.ElementsMatch(t, test.expectedConditions, conditions)

			cgu := &v1alpha1.ClusterGroupUpgrade{}
			for _, expected := range test.expectedCGUJsons {
				c := &unstructured.Unstructured{}
				err := json.Unmarshal([]byte(expected), &c.Object)
				assert.NoError(t, err)

				name, _, _ := unstructured.NestedString(c.Object, "metadata", "name")
				err = reconciler.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: "namespace"}, cgu)
				assert.NoError(t, err)
				json, err := utils.ObjectToJSON(cgu)
				assert.NoError(t, err)
				assert.JSONEq(t, expected, json)
			}
		})
	}
}

// newIBGUForResolution builds an IBGU with the given plan, selecting clusters by
// the "common=true" label used across the resolution tests.
func newIBGUForResolution(plan []ibguv1alpha1.PlanItem) *ibguv1alpha1.ImageBasedGroupUpgrade {
	return &ibguv1alpha1.ImageBasedGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			// A fresh creation time keeps the pre-flight start deadline open by default;
			// tests exercising the deadline override it (the API server always sets this
			// in production).
			CreationTimestamp: metav1.Now(),
		},
		Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
			ClusterLabelSelectors: []v1.LabelSelector{
				{MatchLabels: map[string]string{"common": "true"}},
			},
			Plan: plan,
			IBUSpec: lcav1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1.SeedImageRef{Version: "version", Image: "image"},
			},
		},
	}
}

// completedPrepCGU returns a succeeded plan-item-0 CGU whose spec.clusters is the
// frozen cluster set that later plan items are narrowed from.
func completedPrepCGU(clusters []string) v1alpha1.ClusterGroupUpgrade {
	return v1alpha1.ClusterGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "name-prep-0",
			Namespace: "namespace",
			Labels:    map[string]string{utils.CGUOwnerIBGULabel: "name"},
		},
		Spec: v1alpha1.ClusterGroupUpgradeSpec{
			Clusters:              clusters,
			ManifestWorkTemplates: []string{"name-prep"},
		},
		Status: v1alpha1.ClusterGroupUpgradeStatus{
			Conditions: []v1.Condition{{Type: string(utils.ConditionTypes.Succeeded)}},
		},
	}
}

func managedClusterWithLabels(name string, labels map[string]string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{ObjectMeta: v1.ObjectMeta{Name: name, Labels: labels}}
}

// TestEnsureManifestsFreezesClusterSet verifies that every CGU acts on a subset
// of the set frozen at plan item 0, even when a new ManagedCluster that matches
// the selector is added mid-rollout.
func TestEnsureManifestsFreezesClusterSet(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1", "cluster2"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	// cluster1 and cluster2 are the frozen set; cluster3 is added after freezing
	// and matches both the selector and the Upgrade predicate, but must be ignored.
	for _, name := range []string{"cluster1", "cluster2", "cluster3"} {
		mc := managedClusterWithLabels(name, map[string]string{
			"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})
		assert.NoError(t, fakeClient.Create(context.TODO(), mc))
	}

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	cgu := &v1alpha1.ClusterGroupUpgrade{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu)
	assert.NoError(t, err)
	assert.Equal(t, []string{"cluster1", "cluster2"}, cgu.Spec.Clusters)
	assert.Empty(t, cgu.Spec.ClusterLabelSelectors)
}

// TestEnsureManifestsVanishedCluster verifies that deleting a ManagedCluster
// mid-rollout does not block later plan items: the cluster is excluded from the
// generated CGU (so validateCR does not hard-fail), reported in the IBGU status
// by syncStatusWithCGUs, and a Warning event is emitted.
func TestEnsureManifestsVanishedCluster(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// Frozen set is {cluster1, cluster2, cluster3} but cluster2 no longer exists.
	prepCGU := completedPrepCGU([]string{"cluster1", "cluster2", "cluster3"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	for _, name := range []string{"cluster1", "cluster3"} {
		mc := managedClusterWithLabels(name, map[string]string{
			"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})
		assert.NoError(t, fakeClient.Create(context.TODO(), mc))
	}

	recorder := record.NewFakeRecorder(10)
	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// The generated CGU acts only on the surviving clusters (not blocked).
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu)
	assert.NoError(t, err)
	assert.Equal(t, []string{"cluster1", "cluster3"}, cgu.Spec.Clusters)

	// syncStatusWithCGUs reports the vanished cluster as dropped and emits an event.
	err = reconciler.syncStatusWithCGUs(context.TODO(), ibgu)
	assert.NoError(t, err)
	droppedState := findClusterState(ibgu, "cluster2")
	assert.NotNil(t, droppedState)
	assert.True(t, droppedState.Dropped)
	assert.Equal(t, droppedClusterReason, droppedState.DroppedReason)

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "Warning")
		assert.Contains(t, event, "cluster2")
	default:
		t.Error("expected a Warning event for the dropped cluster")
	}
}

// TestMarkDroppedClustersDurableAndEventOnce verifies the dropped marker is
// re-applied on every reconcile (durable) and the Warning event fires only the
// first time each cluster is observed as dropped.
func TestMarkDroppedClustersDurableAndEventOnce(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
	})

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1", "cluster2"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	// cluster2 does not exist; cluster1 does.
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

	recorder := record.NewFakeRecorder(10)
	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}
	cguList := &v1alpha1.ClusterGroupUpgradeList{}
	assert.NoError(t, reconciler.List(context.TODO(), cguList))

	// First pass: nothing previously dropped -> mark + event.
	assert.NoError(t, reconciler.markDroppedClusters(context.TODO(), ibgu, cguList, droppedClusterNames(ibgu)))
	assert.True(t, findClusterState(ibgu, "cluster2").Dropped)
	assert.Len(t, recorder.Events, 1)
	<-recorder.Events

	// Second pass simulating a later reconcile: syncStatusWithCGUs would have
	// rebuilt Status.Clusters, so re-marking must restore Dropped but NOT re-emit.
	ibgu.Status.Clusters = nil
	prevDropped := map[string]bool{"cluster2": true}
	assert.NoError(t, reconciler.markDroppedClusters(context.TODO(), ibgu, cguList, prevDropped))
	assert.True(t, findClusterState(ibgu, "cluster2").Dropped)
	assert.Len(t, recorder.Events, 0)
}

// TestEnsureManifestsNoEligibleClusters verifies that a plan item narrowing to
// zero eligible clusters (e.g. AbortOnFailure when nothing failed) does not
// create a CGU with an empty spec.clusters and is treated as completed.
func TestEnsureManifestsNoEligibleClusters(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.AbortOnFailure}},
	})

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	// cluster1 exists with no failed label, so AbortOnFailure matches nothing.
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// No CGU is created for the AbortOnFailure item.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{Name: "name-abortonfailure-1", Namespace: "namespace"}, cgu)
	assert.True(t, apierrors.IsNotFound(err))

	// The plan is reported as fully completed.
	completed := false
	for _, c := range ibgu.Status.Conditions {
		if c.Type == string(utils.ConditionTypes.Progressing) &&
			c.Reason == string(utils.ConditionReasons.Completed) {
			completed = true
		}
	}
	assert.True(t, completed)
}

// TestEnsureClusterLabelsSkipsMissingCluster verifies that a cluster deleted
// mid-rollout (still present in status with completed actions) does not fail the
// reconcile, so later plan items are not blocked (regression guard for the
// ManagedCluster-deleted path).
func TestEnsureClusterLabelsSkipsMissingCluster(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
	})
	ibgu.Status.Clusters = []ibguv1alpha1.ClusterState{
		{
			Name:             "gone",
			CompletedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Prep}},
		},
	}

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}

	// "gone" is not a ManagedCluster; this must not error.
	err = reconciler.ensureClusterLabels(context.TODO(), ibgu)
	assert.NoError(t, err)
}

// TestEnsureManifestsCrossIBGUPlan verifies that a standalone Abort IBGU narrows
// its target set from the authoritative ManagedCluster labels written by a prior
// IBGU, so cross-IBGU plans keep working.
func TestEnsureManifestsCrossIBGUPlan(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Abort}},
	})

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// cluster1 has not upgraded (eligible for Abort); cluster2 carries an
	// upgrade-completed label from a prior IBGU (not eligible for Abort).
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster2", map[string]string{
			"common": "true", "lcm.openshift.io/ibgu-upgrade-completed": ""})))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	cgu := &v1alpha1.ClusterGroupUpgrade{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{Name: "name-abort-0", Namespace: "namespace"}, cgu)
	assert.NoError(t, err)
	assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
}

// TestReconcileVanishedClusterDoesNotBlock drives the full Reconcile loop to
// verify end-to-end that deleting a ManagedCluster mid-rollout does not block
// later plan items and that the dropped marker persists across reconciles.
func TestReconcileVanishedClusterDoesNotBlock(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})
	// Plan item 0 already completed, frozen set {cluster1, cluster2}; cluster1 then
	// completed Prep and was deleted, so it lingers in status with a completed action.
	prepCGU := completedPrepCGU([]string{"cluster1", "cluster2"})
	ibgu.Status.Clusters = []ibguv1alpha1.ClusterState{
		{Name: "cluster1", CompletedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Prep}}},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(testscheme).
		WithStatusSubresource(&ibguv1alpha1.ImageBasedGroupUpgrade{}).
		WithObjects(ibgu, &prepCGU,
			managedClusterWithLabels("cluster2", map[string]string{
				"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "name", Namespace: "namespace"}}

	_, err := reconciler.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// The Upgrade CGU for the surviving cluster was created despite cluster1 being gone.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster2"}, cgu.Spec.Clusters)

	// The dropped marker is persisted, and survives a second reconcile.
	updated := &ibguv1alpha1.ImageBasedGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(), req.NamespacedName, updated))
	assert.True(t, findClusterState(updated, "cluster1").Dropped)

	_, err = reconciler.Reconcile(context.TODO(), req)
	assert.NoError(t, err)
	assert.NoError(t, reconciler.Get(context.TODO(), req.NamespacedName, updated))
	assert.True(t, findClusterState(updated, "cluster1").Dropped)
}

// TestResolveClustersUsesSyncedStatus verifies that plan-item eligibility is
// decided from the freshly synced IBGU status as well as the ManagedCluster
// labels: a cluster whose completion was recorded in status (e.g. the final batch
// completing in the same write that set the CGU's Succeeded condition) is eligible
// for the next plan item even though its ManagedCluster label has not been written
// yet.
func TestResolveClustersUsesSyncedStatus(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// The Prep CGU reports cluster1 complete, so ensureManifests' status sync records
	// cluster1 as having completed Prep; its ManagedCluster label has not been written
	// yet, so eligibility must come from the synced status.
	prepCGU := completedPrepCGU([]string{"cluster1"})
	prepCGU.Status.Clusters = []v1alpha1.ClusterState{{Name: "cluster1", State: "complete"}}
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
}

// TestEnsureManifestsResyncsFromOwnSnapshot reproduces the race where a one-cluster
// Prep CGU completes between Reconcile's status sync and ensureManifests' own CGU
// list. The IBGU status entering ensureManifests is stale (it has no record of Prep
// having completed) and the ManagedCluster has no prep-completed label yet, but the
// CGU snapshot ensureManifests lists shows Prep done. Because ensureManifests
// re-syncs status from that same snapshot before evaluating eligibility, the Upgrade
// plan item still resolves cluster1 and its CGU is created — rather than resolving
// empty against the stale status, being skipped, and recording a generation-final
// false success that later reconciles can never undo.
func TestEnsureManifestsResyncsFromOwnSnapshot(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})
	// Stale snapshot: status synced before Prep completed records nothing for cluster1.
	ibgu.Status.Clusters = nil

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// The CGU snapshot ensureManifests lists shows Prep completed for cluster1 (the
	// batch that flipped to complete between the two reads).
	prepCGU := completedPrepCGU([]string{"cluster1"})
	prepCGU.Status.Clusters = []v1alpha1.ClusterState{{Name: "cluster1", State: "complete"}}
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	// cluster1 carries no prep-completed label: ensureClusterLabels ran on the stale
	// snapshot, so the completed action is only visible via the re-synced status.
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// The Upgrade CGU is created for cluster1 and no false completion is recorded.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
	assert.Nil(t, meta.FindStatusCondition(ibgu.Status.Conditions, string(utils.ConditionTypes.Succeeded)))
}

// TestResolveClustersAbortedClusterNotReselected verifies that a cluster which has
// already completed an Abort/Finalize is not re-selected by a later plan item even
// though the synced IBGU status still records its earlier failed action. When the
// abort completed, ensureClusterLabels cleared the cluster's IBGU action labels;
// effectiveActionLabels must not resurrect them from the synced status, or a second
// AbortOnFailure would re-abort a cluster that is already done.
func TestResolveClustersAbortedClusterNotReselected(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.AbortOnFailure}},
	})
	// cluster1 failed Prep earlier and was already aborted (completed AbortOnFailure),
	// so its ManagedCluster action labels have been cleared. cluster2 failed Upgrade
	// and has not been aborted yet.
	ibgu.Status.Clusters = []ibguv1alpha1.ClusterState{
		{Name: "cluster1",
			FailedActions:    []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Prep}},
			CompletedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.AbortOnFailure}}},
		{Name: "cluster2",
			FailedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Upgrade}}},
	}

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1", "cluster2"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	// Neither ManagedCluster carries IBGU action labels: cluster1's were cleared when
	// its abort completed; cluster2's upgrade-failed label has not been written yet.
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster2", map[string]string{"common": "true"})))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	cguList := &v1alpha1.ClusterGroupUpgradeList{}
	assert.NoError(t, reconciler.List(context.TODO(), cguList))

	// Resolve the AbortOnFailure plan item directly from the injected status (the
	// per-cluster state the in-reconcile sync would have produced from the owned CGUs),
	// isolating the eligibility logic: the already-aborted cluster1 must not be
	// re-selected, so only cluster2 is aborted.
	selected, err := reconciler.resolveClustersForPlanItem(
		context.TODO(), ibgu, &ibgu.Spec.Plan[1], cguList)
	assert.NoError(t, err)
	assert.Equal(t, []string{"cluster2"}, selected)
}

// TestReconcileLastBatchCompletesWithSucceeded drives the full Reconcile loop to
// verify that when a plan item's final batch completes in the same status write
// that sets the CGU's Succeeded condition, the next plan item becomes eligible in
// the same reconcile. Reconcile syncs CGU status before resolving manifests, so
// the completion recorded on plan item 0's CGU is visible to plan item 1 without
// waiting a reconcile for the ManagedCluster labels to be written first.
func TestReconcileLastBatchCompletesWithSucceeded(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})

	// Plan item 0's CGU reports Succeeded and, in the same status, both clusters as
	// complete - but the ManagedClusters do not carry the prep-completed label yet.
	prepCGU := completedPrepCGU([]string{"cluster1", "cluster2"})
	prepCGU.Status.Clusters = []v1alpha1.ClusterState{
		{Name: "cluster1", State: utils.ClusterRemediationComplete},
		{Name: "cluster2", State: utils.ClusterRemediationComplete},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(testscheme).
		WithStatusSubresource(&ibguv1alpha1.ImageBasedGroupUpgrade{}).
		WithObjects(ibgu, &prepCGU,
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"}),
			managedClusterWithLabels("cluster2", map[string]string{"common": "true"})).
		Build()

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "name", Namespace: "namespace"}}

	_, err := reconciler.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// The Upgrade CGU is created for both clusters in the same reconcile.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1", "cluster2"}, cgu.Spec.Clusters)
}

// TestEmptySetGateIsTerminal verifies the empty-set pre-flight gate (design D2): on the
// first reconcile it blocks the IBGU terminally (Succeeded=False, Progressing mirrored
// with the same reason), creates no CGU, and returns no error — never a completion. It
// distinguishes two reasons: no ManagedCluster matches the selector
// (NoClustersMatchSelector) and a non-empty base set with no cluster eligible for plan
// item 0's action (NoEligibleClusters). The reference and seed gates have their own
// terminal-block tests (TestEnsureManifestsPrepReferenceGate, TestEnsureManifestsSeedFormatGate).
func TestEmptySetGateIsTerminal(t *testing.T) {
	tests := []struct {
		name    string
		plan    []ibguv1alpha1.PlanItem
		cluster client.Object // nil = no ManagedCluster exists
		reason  utils.ConditionReason
	}{
		{
			name:   "no ManagedCluster matches the selector",
			plan:   []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}},
			reason: utils.ConditionReasons.NoClustersMatchSelector,
		},
		{
			// Plan item 0 is Upgrade, which requires the prep-completed label; the matching
			// cluster lacks it, so the base set is non-empty but none is eligible.
			name:    "no cluster eligible for plan item 0",
			plan:    []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Upgrade}}},
			cluster: managedClusterWithLabels("cluster1", map[string]string{"common": "true"}),
			reason:  utils.ConditionReasons.NoEligibleClusters,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := newIBGUForResolution(test.plan)
			ibgu.CreationTimestamp = metav1.Now()
			fakeClient, err := getFakeClientFromObjects()
			assert.NoError(t, err)
			if test.cluster != nil {
				assert.NoError(t, fakeClient.Create(context.TODO(), test.cluster))
			}
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
				Recorder: record.NewFakeRecorder(10)}

			err = reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)

			succeeded := succeededCondition(ibgu)
			assert.NotNil(t, succeeded)
			assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
			assert.Equal(t, string(test.reason), succeeded.Reason)
			// Progressing is mirrored with the same reason; never a completion.
			assert.Equal(t, string(test.reason), progressingCondition(ibgu).Reason)
			// No CGU was created.
			cguList := &v1alpha1.ClusterGroupUpgradeList{}
			assert.NoError(t, reconciler.List(context.TODO(), cguList))
			assert.Empty(t, cguList.Items)
		})
	}
}

// TestSucceededPresenceIsNotRevived verifies that an IBGU carrying the Succeeded
// condition — whether it finished cleanly (True) or was blocked terminally
// (False) — is not revived by a late-matching ManagedCluster when the spec has not
// changed: a completion whose ObservedGeneration equals the current generation, and
// a permanent block, both short-circuit before any gate or the plan loop. (An
// appended plan item bumps the generation and does resume a completion — see
// TestAppendResumesCompletion — but never a block — see TestBlockedIBGUIgnoresAppend.)
func TestSucceededPresenceIsNotRevived(t *testing.T) {
	variants := []struct {
		name   string
		status metav1.ConditionStatus
		reason utils.ConditionReason
	}{
		{"finished cleanly", metav1.ConditionTrue, utils.ConditionReasons.Completed},
		{"blocked terminally", metav1.ConditionFalse, utils.ConditionReasons.NoEligibleClusters},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			// A plan whose spec has not changed since completion (generation 0, the
			// completion's ObservedGeneration 0) and a late cluster that would match the
			// selector and the Upgrade predicate.
			ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
				{Actions: []string{ibguv1alpha1.Prep}},
				{Actions: []string{ibguv1alpha1.Upgrade}},
			})
			utils.SetStatusCondition(&ibgu.Status.Conditions,
				utils.ConditionTypes.Succeeded, v.reason, v.status, "terminal")

			fakeClient, err := getFakeClientFromObjects()
			assert.NoError(t, err)
			assert.NoError(t, fakeClient.Create(context.TODO(),
				managedClusterWithLabels("late", map[string]string{
					"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))

			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			err = reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)

			// No CGU is created for either plan item.
			for _, name := range []string{"name-prep-0", "name-upgrade-1"} {
				cgu := &v1alpha1.ClusterGroupUpgrade{}
				assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
					types.NamespacedName{Name: name, Namespace: "namespace"}, cgu)))
			}
		})
	}
}

// TestCompletedWithFailuresAtEndOfPlan verifies that a plan running to the end with
// a cluster that recorded a failed action reports Succeeded=False, reason
// CompletedWithFailures (a clean run with a finalizing tail reports Succeeded=True,
// Completed).
func TestCompletedWithFailuresAtEndOfPlan(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// A completed plan-item-0 CGU exists (plan runs to the end), but cluster1 timed out
	// on prep; ensureManifests' status sync derives the failed action from the CGU.
	prepCGU := completedPrepCGU([]string{"cluster1"})
	prepCGU.Status.Clusters = []v1alpha1.ClusterState{{
		Name: "cluster1", State: "timedout",
		CurrentManifestWork: &v1alpha1.ManifestWorkStatus{Name: "ibu-prep"},
	}}
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
	assert.Equal(t, string(utils.ConditionReasons.CompletedWithFailures), succeeded.Reason)
	assert.Equal(t, string(utils.ConditionReasons.CompletedWithFailures), progressingCondition(ibgu).Reason)
}

// TestPlanItemCGUFailedIsCompletedWithFailures verifies CodeRabbit finding #4: a
// plan-item CGU that ended with a terminal Succeeded=False condition but recorded no
// per-cluster failed action (e.g. precaching with no available cluster, or a batch
// timeout before any cluster started) still makes the plan completion
// CompletedWithFailures rather than a clean Succeeded=True.
func TestPlanItemCGUFailedIsCompletedWithFailures(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	// No failed action is recorded in the IBGU status; the failure lives only on the
	// plan-item CGU's Succeeded=False condition.

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	failedPrepCGU := v1alpha1.ClusterGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "name-prep-0",
			Namespace: "namespace",
			Labels:    map[string]string{utils.CGUOwnerIBGULabel: "name"},
		},
		Spec: v1alpha1.ClusterGroupUpgradeSpec{
			Clusters:              []string{"cluster1"},
			ManifestWorkTemplates: []string{"name-prep"},
		},
		Status: v1alpha1.ClusterGroupUpgradeStatus{
			Conditions: []v1.Condition{{
				Type:   string(utils.ConditionTypes.Succeeded),
				Status: metav1.ConditionFalse,
				Reason: string(utils.ConditionReasons.TimedOut),
			}},
		},
	}
	assert.NoError(t, fakeClient.Create(context.TODO(), &failedPrepCGU))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
	assert.Equal(t, string(utils.ConditionReasons.CompletedWithFailures), succeeded.Reason)
}

// TestEnsureCGUForPlanItemClusterSets verifies the cluster set each plan item's CGU
// targets after the pre-flight was simplified (CNF-30176): plan item 0 is created
// from the eligible set the empty-set gate already resolved — passed in verbatim and
// never re-resolved, so the gate and the first CGU can never disagree and there is no
// empty-set race to re-check — while a later plan item that legitimately narrows to no
// eligible cluster is treated as a completion rather than creating an empty CGU.
func TestEnsureCGUForPlanItemClusterSets(t *testing.T) {
	t.Run("plan item 0 targets exactly the gate's eligible set", func(t *testing.T) {
		// The gate resolved cluster1 as eligible for plan item 0; the CGU must be created
		// from that set verbatim. cluster2 exists and matches the selector but was not in
		// the gate's set, so it must not be pulled into the CGU by any re-resolution.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster2", map[string]string{"common": "true"})))
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}

		cguList := &v1alpha1.ClusterGroupUpgradeList{}
		completed, err := reconciler.ensureCGUForPlanItem(
			context.TODO(), ibgu, &ibgu.Spec.Plan[0], 0, cguList, []string{"cluster1"})
		assert.NoError(t, err)
		assert.False(t, completed)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.NoError(t, reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu))
		assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
	})

	t.Run("plan item 0 does not re-resolve: a cluster deleted after the gate is still targeted", func(t *testing.T) {
		// The gate resolved cluster1, but it has since been deleted (absent from the
		// client). ensureCGUForPlanItem must still create plan item 0's CGU from the
		// gate's set verbatim — proving there is no second resolution or empty-set
		// re-check that a deleted cluster could turn into a requeue or a terminal block.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}

		cguList := &v1alpha1.ClusterGroupUpgradeList{}
		completed, err := reconciler.ensureCGUForPlanItem(
			context.TODO(), ibgu, &ibgu.Spec.Plan[0], 0, cguList, []string{"cluster1"})
		assert.NoError(t, err)
		assert.False(t, completed)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.NoError(t, reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu))
		assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
	})

	t.Run("later plan item narrowing to zero is a completion", func(t *testing.T) {
		// Plan item 1 is AbortOnFailure; the frozen cluster completed Prep with no
		// failure, so it is not eligible for AbortOnFailure and the item narrows to zero.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
			{Actions: []string{ibguv1alpha1.Prep}},
			{Actions: []string{ibguv1alpha1.AbortOnFailure}},
		})
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		prepCGU := completedPrepCGU([]string{"cluster1"})
		assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
		assert.NoError(t, fakeClient.Create(context.TODO(), managedClusterWithLabels("cluster1",
			map[string]string{"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}

		cguList := &v1alpha1.ClusterGroupUpgradeList{}
		assert.NoError(t, reconciler.List(context.TODO(), cguList))
		// item0Clusters is nil: a later plan item never consults it.
		completed, err := reconciler.ensureCGUForPlanItem(context.TODO(), ibgu, &ibgu.Spec.Plan[1], 1, cguList, nil)
		assert.NoError(t, err)
		assert.True(t, completed)
	})
}

// TestStartDeadline proves AC 4: a passing pre-flight honours the fixed 5-minute start
// deadline. If the first CGU would be created more than preflightStartDeadline after
// creation (e.g. TALM was down past it and recovered), the IBGU is blocked with
// PreflightWindowExpired and no CGU; within the deadline the rollout proceeds and the
// first CGU is created.
func TestStartDeadline(t *testing.T) {
	tests := []struct {
		name        string
		createdAgo  time.Duration
		expectBlock bool
	}{
		{name: "expired blocks even when gates pass", createdAgo: preflightStartDeadline + time.Minute, expectBlock: true},
		{name: "within deadline proceeds", createdAgo: 0, expectBlock: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
			ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-test.createdAgo))

			fakeClient, err := getFakeClientFromObjects()
			assert.NoError(t, err)
			// A cluster eligible for Prep so all gates pass.
			assert.NoError(t, fakeClient.Create(context.TODO(),
				managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
				Recorder: record.NewFakeRecorder(10)}

			err = reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)

			cgu := &v1alpha1.ClusterGroupUpgrade{}
			getErr := reconciler.Get(context.TODO(),
				types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
			if test.expectBlock {
				succeeded := succeededCondition(ibgu)
				assert.NotNil(t, succeeded)
				assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
				assert.Equal(t, string(utils.ConditionReasons.PreflightWindowExpired), succeeded.Reason)
				// No CGU was created.
				assert.True(t, apierrors.IsNotFound(getErr))
			} else {
				// Not blocked: no Succeeded condition, and the first CGU was created.
				assert.Nil(t, succeededCondition(ibgu))
				assert.NoError(t, getErr)
				assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
			}
		})
	}
}

// TestUncachedCGUListDefeatsStaleTerminalBlock proves the cache-lag guard on the
// terminal pre-flight gates: when the cached owned-CGU list is empty but the uncached
// reader shows plan item 0's CGU already exists, ensureManifests must not run the gates
// or the start deadline. Without the guard, an IBGU created long ago (past the 5-minute
// start deadline) whose CGU has not yet landed in the informer cache would be blocked
// with PreflightWindowExpired even though its first CGU already exists — wedging it
// permanently. With the guard, the uncached snapshot is adopted, no block is recorded,
// and the plan loop simply waits on the existing CGU.
func TestUncachedCGUListDefeatsStaleTerminalBlock(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	// Older than the fixed start deadline: if the gates ran and passed, the deadline
	// branch would record PreflightWindowExpired.
	ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-(preflightStartDeadline + time.Minute)))

	// Cached client: the CGU has not yet landed (cache lag). A matching cluster is
	// present so, if the gates wrongly ran, the empty-set gate would pass and the
	// deadline would then fire.
	cachedClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)

	// Uncached reader: plan item 0's CGU already exists (not yet completed), plus the
	// same matching cluster.
	uncachedReader, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	inProgressPrepCGU := &v1alpha1.ClusterGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "name-prep-0",
			Namespace: "namespace",
			Labels:    map[string]string{utils.CGUOwnerIBGULabel: "name"},
		},
		Spec: v1alpha1.ClusterGroupUpgradeSpec{
			Clusters:              []string{"cluster1"},
			ManifestWorkTemplates: []string{"name-prep"},
		},
	}
	assert.NoError(t, uncachedReader.Create(context.TODO(), inProgressPrepCGU))
	for _, c := range []client.WithWatch{cachedClient, uncachedReader} {
		assert.NoError(t, c.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
	}

	// A seed reader that would fail if consulted, to prove the gates never ran.
	reader := &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")}
	reconciler := IBGUReconciler{
		Client: cachedClient, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
		Recorder: record.NewFakeRecorder(10), SeedImageReader: reader,
	}

	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// No terminal block was recorded (neither PreflightWindowExpired nor an empty-set
	// block): the uncached reader showed the CGU already exists.
	assert.Nil(t, succeededCondition(ibgu))
	// The gates never ran: no registry read.
	assert.False(t, reader.called)
	// The plan loop waited on the existing (uncached) CGU.
	assert.Equal(t, string(utils.ConditionReasons.InProgress), progressingCondition(ibgu).Reason)
}

// ownedCGU returns an owned CGU for the IBGU "name" with a terminal Succeeded
// condition, for the plan-item-CGU-sequence gap tests.
func ownedCGU(name string, clusters []string) *v1alpha1.ClusterGroupUpgrade {
	return &v1alpha1.ClusterGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: "namespace",
			Labels:    map[string]string{utils.CGUOwnerIBGULabel: "name"},
		},
		Spec: v1alpha1.ClusterGroupUpgradeSpec{Clusters: clusters},
		Status: v1alpha1.ClusterGroupUpgradeStatus{
			Conditions: []v1.Condition{{Type: string(utils.ConditionTypes.Succeeded)}},
		},
	}
}

// TestPlanCGUMissingBlocksTerminally verifies the Developer of Record's gap gate: when a
// later plan item's CGU exists but an earlier one is absent (deleted out of band),
// ensureManifests blocks terminally with PlanCGUMissing and creates no CGU — rather than
// treating item 0 as a completed empty step (stamping a completion for clusters that
// never ran it) or silently recreating a middle item's CGU and re-running its action
// against current labels. Plan item 0's CGU is the carrier of the unrecoverable frozen
// cluster set, so its absence is the important case, but any earlier gap is terminal.
func TestPlanCGUMissingBlocksTerminally(t *testing.T) {
	tests := []struct {
		name       string
		plan       []ibguv1alpha1.PlanItem
		existing   []string // owned CGUs present
		missingCGU string   // the CGU that must not be (re)created
	}{
		{
			name: "plan item 0 CGU missing (frozen-set carrier)",
			plan: []ibguv1alpha1.PlanItem{
				{Actions: []string{ibguv1alpha1.Prep}},
				{Actions: []string{ibguv1alpha1.Upgrade}},
			},
			existing:   []string{"name-upgrade-1"},
			missingCGU: "name-prep-0",
		},
		{
			name: "middle plan item CGU missing",
			plan: []ibguv1alpha1.PlanItem{
				{Actions: []string{ibguv1alpha1.Prep}},
				{Actions: []string{ibguv1alpha1.Upgrade}},
				{Actions: []string{ibguv1alpha1.FinalizeUpgrade}},
			},
			existing:   []string{"name-prep-0", "name-finalizeupgrade-2"},
			missingCGU: "name-upgrade-1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := newIBGUForResolution(test.plan)

			fakeClient, err := getFakeClientFromObjects()
			assert.NoError(t, err)
			for _, name := range test.existing {
				assert.NoError(t, fakeClient.Create(context.TODO(), ownedCGU(name, []string{"cluster1"})))
			}

			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
				Recorder: record.NewFakeRecorder(10)}
			err = reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)

			succeeded := succeededCondition(ibgu)
			assert.NotNil(t, succeeded)
			assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
			assert.Equal(t, string(utils.ConditionReasons.PlanCGUMissing), succeeded.Reason)
			// No CGU was created for the missing plan item.
			cgu := &v1alpha1.ClusterGroupUpgrade{}
			assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
				types.NamespacedName{Name: test.missingCGU, Namespace: "namespace"}, cgu)))
		})
	}
}

// TestCacheLaggedCGUSequenceDoesNotBlock verifies the gap gate is confirmed through the
// uncached reader: a cached list that only shows plan item 1's CGU (item 0's has not yet
// landed in the informer cache) looks like a gap, but the uncached reader has the full
// sequence, so no PlanCGUMissing block is recorded and the plan loop proceeds.
func TestCacheLaggedCGUSequenceDoesNotBlock(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})

	// Cached client: only plan item 1's CGU has landed — item 0's lags the informer.
	cachedClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	assert.NoError(t, cachedClient.Create(context.TODO(), ownedCGU("name-upgrade-1", []string{"cluster1"})))

	// Uncached reader: the full sequence exists (no real gap). Plan item 0's CGU is
	// completed so the plan loop advances past it; plan item 1's CGU is still in progress
	// (no Succeeded condition), so the loop waits on it rather than completing.
	uncachedReader, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1"})
	assert.NoError(t, uncachedReader.Create(context.TODO(), &prepCGU))
	inProgressUpgrade := ownedCGU("name-upgrade-1", []string{"cluster1"})
	inProgressUpgrade.Status.Conditions = nil
	assert.NoError(t, uncachedReader.Create(context.TODO(), inProgressUpgrade))
	for _, c := range []client.WithWatch{cachedClient, uncachedReader} {
		assert.NoError(t, c.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
	}

	reconciler := IBGUReconciler{
		Client: cachedClient, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
		Recorder: record.NewFakeRecorder(10),
	}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// No terminal block: the uncached reader showed the full CGU sequence.
	assert.Nil(t, succeededCondition(ibgu))
	assert.Equal(t, string(utils.ConditionReasons.InProgress), progressingCondition(ibgu).Reason)
}

// TestResumesOnAppend verifies the Developer of Record's append design: an IBGU whose
// completion was recorded against an earlier generation resumes when a plan item is
// appended (the generation bumps). The Succeeded condition is dropped, Progressing goes
// InProgress, the appended item's CGU is created on the frozen cluster set, and the
// pre-flight gates do not run again. It holds for both a clean Completed and a
// CompletedWithFailures completion (the latter resuming into a recovery item).
func TestResumesOnAppend(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T) (*ibguv1alpha1.ImageBasedGroupUpgrade, client.WithWatch)
		appendedCGU    string
		expectClusters []string // nil skips the cluster-set assertion
	}{
		{
			name: "Completed resumes when an Upgrade item is appended",
			setup: func(t *testing.T) (*ibguv1alpha1.ImageBasedGroupUpgrade, client.WithWatch) {
				ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
					{Actions: []string{ibguv1alpha1.Prep}},
					{Actions: []string{ibguv1alpha1.Upgrade}},
				})
				// The spec was edited to append the Upgrade item, so the generation is now 2
				// while the completion was recorded at generation 1.
				ibgu.Generation = 2
				utils.SetStatusCondition(&ibgu.Status.Conditions,
					utils.ConditionTypes.Succeeded, utils.ConditionReasons.Completed,
					metav1.ConditionTrue, "All plan steps are completed")
				setConditionObservedGeneration(&ibgu.Status.Conditions, utils.ConditionTypes.Succeeded, 1)

				fakeClient, err := getFakeClientFromObjects()
				assert.NoError(t, err)
				prepCGU := completedPrepCGU([]string{"cluster1"})
				assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
				assert.NoError(t, fakeClient.Create(context.TODO(), managedClusterWithLabels("cluster1",
					map[string]string{"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))
				return ibgu, fakeClient
			},
			appendedCGU:    "name-upgrade-1",
			expectClusters: []string{"cluster1"},
		},
		{
			name: "CompletedWithFailures resumes when AbortOnFailure is appended",
			setup: func(t *testing.T) (*ibguv1alpha1.ImageBasedGroupUpgrade, client.WithWatch) {
				ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
					{Actions: []string{ibguv1alpha1.Prep, ibguv1alpha1.Upgrade}},
					{Actions: []string{ibguv1alpha1.AbortOnFailure}},
				})
				ibgu.Generation = 2
				// The first item recorded a failed Upgrade action on the cluster.
				ibgu.Status.Clusters = []ibguv1alpha1.ClusterState{
					{Name: "cluster1", FailedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Upgrade}}},
				}
				utils.SetStatusCondition(&ibgu.Status.Conditions,
					utils.ConditionTypes.Succeeded, utils.ConditionReasons.CompletedWithFailures,
					metav1.ConditionFalse, "completed with failures")
				setConditionObservedGeneration(&ibgu.Status.Conditions, utils.ConditionTypes.Succeeded, 1)

				fakeClient, err := getFakeClientFromObjects()
				assert.NoError(t, err)
				prepUpgradeCGU := v1alpha1.ClusterGroupUpgrade{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep-upgrade-0",
						Namespace: "namespace",
						Labels:    map[string]string{utils.CGUOwnerIBGULabel: "name"},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						Clusters:              []string{"cluster1"},
						ManifestWorkTemplates: []string{"name-prep", "name-upgrade"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Conditions: []v1.Condition{{Type: string(utils.ConditionTypes.Succeeded)}},
					},
				}
				assert.NoError(t, fakeClient.Create(context.TODO(), &prepUpgradeCGU))
				// The cluster carries the failed-upgrade label, so it is eligible for AbortOnFailure.
				assert.NoError(t, fakeClient.Create(context.TODO(), managedClusterWithLabels("cluster1",
					map[string]string{"common": "true", "lcm.openshift.io/ibgu-upgrade-failed": ""})))
				return ibgu, fakeClient
			},
			appendedCGU: "name-abortonfailure-1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu, fakeClient := test.setup(t)

			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			err := reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)

			// The completion Succeeded condition is dropped so the IBGU resumes.
			assert.Nil(t, succeededCondition(ibgu))
			assert.Equal(t, string(utils.ConditionReasons.InProgress), progressingCondition(ibgu).Reason)
			// The appended item's CGU is created (on the frozen cluster set from plan item 0).
			cgu := &v1alpha1.ClusterGroupUpgrade{}
			assert.NoError(t, reconciler.Get(context.TODO(),
				types.NamespacedName{Name: test.appendedCGU, Namespace: "namespace"}, cgu))
			if test.expectClusters != nil {
				assert.Equal(t, test.expectClusters, cgu.Spec.Clusters)
			}
		})
	}
}

// TestBlockedIBGUIgnoresAppend verifies that a permanent pre-flight block is never
// resumed by an appended plan item (a generation bump), unlike a completion.
func TestBlockedIBGUIgnoresAppend(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})
	ibgu.Generation = 2
	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.Succeeded, utils.ConditionReasons.NoEligibleClusters,
		metav1.ConditionFalse, "blocked")
	setConditionObservedGeneration(&ibgu.Status.Conditions, utils.ConditionTypes.Succeeded, 1)

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	assert.NoError(t, fakeClient.Create(context.TODO(), managedClusterWithLabels("cluster1",
		map[string]string{"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// The block is unchanged and no CGU is created.
	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, string(utils.ConditionReasons.NoEligibleClusters), succeeded.Reason)
	for _, name := range []string{"name-prep-0", "name-upgrade-1"} {
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
			types.NamespacedName{Name: name, Namespace: "namespace"}, cgu)))
	}
}

// TestCompletedRestartNoAppendStaysFinished verifies that a completed IBGU whose
// completion matches the current generation (no append) stays finished across a
// restart: it short-circuits and creates no CGU.
func TestCompletedRestartNoAppendStaysFinished(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	ibgu.Generation = 1
	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.Succeeded, utils.ConditionReasons.Completed,
		metav1.ConditionTrue, "All plan steps are completed")
	setConditionObservedGeneration(&ibgu.Status.Conditions, utils.ConditionTypes.Succeeded, 1)

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	assert.NoError(t, fakeClient.Create(context.TODO(), managedClusterWithLabels("cluster1",
		map[string]string{"common": "true"})))

	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// Still finished; the Succeeded condition is unchanged.
	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, metav1.ConditionTrue, succeeded.Status)
	assert.Equal(t, string(utils.ConditionReasons.Completed), succeeded.Reason)
}

// TestGateOrderEmptyBeforeReferenceBeforeSeed verifies that the hub-only
// deterministic checks run before the network read: a plan with both an empty
// selector and a missing reference reports the empty-set reason and performs no
// registry read.
func TestGateOrderEmptyBeforeReferenceBeforeSeed(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc-missing"}}
	ibgu.CreationTimestamp = metav1.Now()

	// No ManagedCluster is created, so the empty-set gate (run first) fails.
	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	reader := &fakeSeedImageReader{format: preBumpFormat}
	reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
		Recorder: record.NewFakeRecorder(10), SeedImageReader: reader}

	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	// The terminal block carries the empty-set reason, not the reference reason.
	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, string(utils.ConditionReasons.NoClustersMatchSelector), succeeded.Reason)
	assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), succeeded.Reason)
	// The registry was never read: the empty-set gate short-circuited before the seed gate.
	assert.False(t, reader.called)
}

// findClusterState returns the ClusterState for the named cluster, or nil.
func findClusterState(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, name string) *ibguv1alpha1.ClusterState {
	for i := range ibgu.Status.Clusters {
		if ibgu.Status.Clusters[i].Name == name {
			return &ibgu.Status.Clusters[i]
		}
	}
	return nil
}

// hubBackupContent returns a hub-side BackupContent CR in the IBGU's namespace.
func hubBackupContent(name, namespace string) *ibguv1alpha1.BackupContent {
	return &ibguv1alpha1.BackupContent{
		ObjectMeta: v1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

// oadpConfigMap returns a ConfigMap used as an oadpContent reference.
func oadpConfigMap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: v1.ObjectMeta{Name: name, Namespace: namespace}}
}

func TestPlanHasPrep(t *testing.T) {
	tests := []struct {
		name     string
		plan     []ibguv1alpha1.PlanItem
		expected bool
	}{
		{
			name:     "prep only",
			plan:     []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}},
			expected: true,
		},
		{
			name: "prep combined with upgrade",
			plan: []ibguv1alpha1.PlanItem{
				{Actions: []string{ibguv1alpha1.Prep, ibguv1alpha1.Upgrade}},
			},
			expected: true,
		},
		{
			name: "prep in a later plan item",
			plan: []ibguv1alpha1.PlanItem{
				{Actions: []string{ibguv1alpha1.AbortOnFailure}},
				{Actions: []string{ibguv1alpha1.Prep}},
			},
			expected: true,
		},
		{
			name:     "upgrade only",
			plan:     []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Upgrade}}},
			expected: false,
		},
		{
			name: "abort only",
			plan: []ibguv1alpha1.PlanItem{
				{Actions: []string{ibguv1alpha1.Abort}},
			},
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := newIBGUForResolution(test.plan)
			assert.Equal(t, test.expected, planHasPrep(ibgu))
		})
	}
}

func TestResolvePrepReferences(t *testing.T) {
	tests := []struct {
		name          string
		backupContent []lcav1.BackupContentRef
		oadpContent   []lcav1.ConfigMapRef
		existingBC    []*ibguv1alpha1.BackupContent
		existingCM    []*corev1.ConfigMap
		expected      []string
	}{
		{
			name:          "all backupContent present",
			backupContent: []lcav1.BackupContentRef{{Name: "bc1"}, {Name: "bc2"}},
			existingBC: []*ibguv1alpha1.BackupContent{
				hubBackupContent("bc1", "namespace"),
				hubBackupContent("bc2", "namespace"),
			},
			expected: nil,
		},
		{
			name:          "one backupContent missing",
			backupContent: []lcav1.BackupContentRef{{Name: "bc1"}, {Name: "bc2"}},
			existingBC:    []*ibguv1alpha1.BackupContent{hubBackupContent("bc1", "namespace")},
			expected:      []string{`BackupContent "bc2"`},
		},
		{
			name:          "both backupContent missing",
			backupContent: []lcav1.BackupContentRef{{Name: "bc1"}, {Name: "bc2"}},
			expected:      []string{`BackupContent "bc1"`, `BackupContent "bc2"`},
		},
		{
			name:          "backupContent and oadpContent both present",
			backupContent: []lcav1.BackupContentRef{{Name: "bc1"}},
			oadpContent:   []lcav1.ConfigMapRef{{Name: "cm1", Namespace: "ns1"}},
			existingBC:    []*ibguv1alpha1.BackupContent{hubBackupContent("bc1", "namespace")},
			existingCM:    []*corev1.ConfigMap{oadpConfigMap("cm1", "ns1")},
			expected:      nil,
		},
		{
			name:        "oadpContent configmap missing",
			oadpContent: []lcav1.ConfigMapRef{{Name: "cm1", Namespace: "ns1"}},
			expected:    []string{`ConfigMap "ns1/cm1"`},
		},
		{
			name:          "mixed missing across both kinds, sorted",
			backupContent: []lcav1.BackupContentRef{{Name: "bc2"}, {Name: "bc1"}},
			oadpContent:   []lcav1.ConfigMapRef{{Name: "cm1", Namespace: "ns1"}},
			existingBC:    []*ibguv1alpha1.BackupContent{hubBackupContent("bc1", "namespace")},
			expected:      []string{`BackupContent "bc2"`, `ConfigMap "ns1/cm1"`},
		},
		{
			name:          "backupContent in wrong namespace treated as missing",
			backupContent: []lcav1.BackupContentRef{{Name: "bc1"}},
			existingBC:    []*ibguv1alpha1.BackupContent{hubBackupContent("bc1", "other-namespace")},
			expected:      []string{`BackupContent "bc1"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
			ibgu.Spec.IBUSpec.BackupContent = test.backupContent
			ibgu.Spec.IBUSpec.OADPContent = test.oadpContent

			fakeClient, err := getFakeClientFromObjects()
			assert.NoError(t, err)
			for _, bc := range test.existingBC {
				assert.NoError(t, fakeClient.Create(context.TODO(), bc))
			}
			for _, cm := range test.existingCM {
				assert.NoError(t, fakeClient.Create(context.TODO(), cm))
			}

			reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			missing, err := reconciler.resolvePrepReferences(context.TODO(), reconciler.preflightReader(), ibgu)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, missing)
		})
	}
}

// progressingCondition returns the Progressing condition, or nil.
func progressingCondition(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) *metav1.Condition {
	for i := range ibgu.Status.Conditions {
		if ibgu.Status.Conditions[i].Type == string(utils.ConditionTypes.Progressing) {
			return &ibgu.Status.Conditions[i]
		}
	}
	return nil
}

// succeededCondition returns the terminal Succeeded condition, or nil. Its presence
// (True or False) marks the IBGU as finished.
func succeededCondition(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) *metav1.Condition {
	for i := range ibgu.Status.Conditions {
		if ibgu.Status.Conditions[i].Type == string(utils.ConditionTypes.Succeeded) {
			return &ibgu.Status.Conditions[i]
		}
	}
	return nil
}

// TestEnsureManifestsPrepReferenceGate verifies the pre-flight reference-resolution
// gate: a Prep plan with an unresolvable reference is blocked terminally before any
// CGU is created, while resolvable references, Upgrade-only plans, and
// already-in-progress IBGUs are not blocked. The BackupContent terminal arc is also
// covered by TestMissingReferenceIsTerminalImmediately; the missing-ConfigMap subtest
// here covers the oadpContent reference path.
func TestEnsureManifestsPrepReferenceGate(t *testing.T) {
	t.Run("prep plan with missing reference is terminal and creates no CGU", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc-missing"}}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		// A compatible seed reader is injected so the seed-format gate passes and
		// this test continues to exercise the reference gate.
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), succeeded.Reason)
		assert.Contains(t, succeeded.Message, `BackupContent "bc-missing"`)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), progressingCondition(ibgu).Reason)

		// No CGU was created.
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("prep plan with resolvable references is not blocked and creates a CGU", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc1"}}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(), hubBackupContent("bc1", "namespace")))
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		// A compatible seed reader is injected so the seed-format gate passes and
		// this test continues to exercise the reference gate.
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.NoError(t, err)
	})

	t.Run("prep plan with missing oadpContent configmap is terminal and creates no CGU", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.OADPContent = []lcav1.ConfigMapRef{{Name: "cm-missing", Namespace: "ns1"}}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder: record.NewFakeRecorder(10)}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), succeeded.Reason)
		assert.Contains(t, succeeded.Message, `ConfigMap "ns1/cm-missing"`)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), progressingCondition(ibgu).Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("upgrade-only plan is not blocked by missing hub entries", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Upgrade}}})
		// A missing hub BackupContent must not block an Upgrade-only follow-up plan.
		ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc-missing"}}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// cluster1 completed Prep in a prior IBGU, so it is eligible for Upgrade.
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{
				"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))

		// backupContent is set on this Upgrade-only plan, so inject a compatible seed
		// reader to isolate the reference-gate behaviour under test.
		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-upgrade-0", Namespace: "namespace"}, cgu)
		assert.NoError(t, err)
	})

	t.Run("in-progress IBGU with missing reference is not blocked", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc-missing"}}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// A CGU already exists for plan item 0, so the gate does not run.
		prepCGU := completedPrepCGU([]string{"cluster1"})
		assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
	})

	t.Run("prep plan with a pullSecretRef naming a missing secret is terminal and creates no CGU", func(t *testing.T) {
		// An oadpContent-only IBGU (no backupContent, so the seed gate does not read the
		// pull secret) with a pullSecretRef naming a secret that does not exist on the hub.
		// The reference gate must catch the missing secret and block terminally, rather than
		// letting the Prep CGU be created without it.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.OADPContent = []lcav1.ConfigMapRef{{Name: "cm1", Namespace: "ns1"}}
		ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "pull-missing"}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
		assert.NoError(t, fakeClient.Create(context.TODO(), &corev1.ConfigMap{
			ObjectMeta: v1.ObjectMeta{Name: "cm1", Namespace: "ns1"}}))

		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder: record.NewFakeRecorder(10)}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), succeeded.Reason)
		assert.Contains(t, succeeded.Message, `Secret "pull-missing"`)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("prep plan with no pullSecretRef is not gated on a pull secret", func(t *testing.T) {
		// A plan that copies no pull secret must not be blocked by the pull-secret check.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.NoError(t, reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu))
	})

	t.Run("a transient pull-secret read failure returns an error and creates no CGU", func(t *testing.T) {
		// A non-NotFound read failure on the pull secret must requeue (return an error),
		// not silently drop the secret and create an incomplete Prep CGU.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "pull"}

		base, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, base.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
		faultyClient := interceptor.NewClient(base, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					return apierrors.NewInternalError(fmt.Errorf("etcd unavailable"))
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})

		reconciler := IBGUReconciler{Client: faultyClient, APIReader: faultyClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder: record.NewFakeRecorder(10)}
		err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.Error(t, err)

		// No terminal block was recorded for a transient fault, and no CGU was created.
		assert.Nil(t, succeededCondition(ibgu))
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)))
	})
}
