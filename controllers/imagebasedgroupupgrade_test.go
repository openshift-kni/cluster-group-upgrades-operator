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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcabcv1 "github.com/openshift-kni/lifecycle-agent/api/backupcontent/v1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
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
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			manifest := reconciler.getSecretManifest(context.TODO(), ibgu)
			assert.JSONEq(t, test.expectedJson, string(manifest.Raw))
		})
	}
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
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
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
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
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
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
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
					// A fresh creation time keeps the pre-flight grace window open so the
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
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}

			_, err = reconciler.ensureManifests(context.TODO(), ibgu)
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
			// A fresh creation time keeps the pre-flight grace window open by default;
			// tests exercising the window override it (the API server always sets this
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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	_, err = reconciler.ensureManifests(context.TODO(), ibgu)
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
	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}
	_, err = reconciler.ensureManifests(context.TODO(), ibgu)
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
	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}
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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	_, err = reconciler.ensureManifests(context.TODO(), ibgu)
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
	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}

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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	_, err = reconciler.ensureManifests(context.TODO(), ibgu)
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
	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}
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
	// Synced status records cluster1 as having completed Prep; its ManagedCluster
	// label has not been written yet.
	ibgu.Status.Clusters = []ibguv1alpha1.ClusterState{
		{Name: "cluster1", CompletedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Prep}}},
	}

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	_, err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	_, err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-abortonfailure-1", Namespace: "namespace"}, cgu))
	// Only cluster2 is aborted; the already-aborted cluster1 is not re-selected.
	assert.Equal(t, []string{"cluster2"}, cgu.Spec.Clusters)
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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "name", Namespace: "namespace"}}

	_, err := reconciler.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// The Upgrade CGU is created for both clusters in the same reconcile.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1", "cluster2"}, cgu.Spec.Clusters)
}

// TestEmptySelectorTransientThenTerminal verifies the empty-set gate (design D2)
// when no ManagedCluster matches the selector: within the grace window it retries
// (transient, requeue, no terminal state), and once the window has elapsed it
// becomes terminal (Succeeded=False, reason NoClustersMatchSelector) and creates no
// CGU.
func TestEmptySelectorTransientThenTerminal(t *testing.T) {
	newEmptyIBGU := func() *ibguv1alpha1.ImageBasedGroupUpgrade {
		// A Prep plan whose selector matches no ManagedCluster (none are created).
		return newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	}

	t.Run("within grace: retries", func(t *testing.T) {
		ibgu := newEmptyIBGU()
		ibgu.CreationTimestamp = metav1.Now()
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked)
		assert.Nil(t, succeededCondition(ibgu))
		assert.Equal(t, string(utils.ConditionReasons.NoClustersMatchSelector), progressingCondition(ibgu).Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)))
	})

	t.Run("past grace: terminal", func(t *testing.T) {
		ibgu := newEmptyIBGU()
		ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-10 * time.Minute))
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder: record.NewFakeRecorder(10)}

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.NoClustersMatchSelector), succeeded.Reason)
		// Progressing is mirrored with the same reason so existing consumers see it stopped.
		assert.Equal(t, string(utils.ConditionReasons.NoClustersMatchSelector), progressingCondition(ibgu).Reason)
	})
}

// TestNoEligibleAtItem0TransientThenTerminal verifies the empty-set gate when the
// base set is non-empty but no cluster is eligible for plan item 0's action
// (reason NoEligibleClusters).
func TestNoEligibleAtItem0TransientThenTerminal(t *testing.T) {
	// Plan item 0 is Upgrade, which requires the prep-completed label; the matching
	// cluster does not carry it, so the base set is non-empty but none is eligible.
	newIBGU := func() *ibguv1alpha1.ImageBasedGroupUpgrade {
		return newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Upgrade}}})
	}
	setup := func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) IBGUReconciler {
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
		return IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder: record.NewFakeRecorder(10)}
	}

	t.Run("within grace: retries", func(t *testing.T) {
		ibgu := newIBGU()
		ibgu.CreationTimestamp = metav1.Now()
		reconciler := setup(ibgu)

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked)
		assert.Nil(t, succeededCondition(ibgu))
		assert.Equal(t, string(utils.ConditionReasons.NoEligibleClusters), progressingCondition(ibgu).Reason)
	})

	t.Run("past grace: terminal", func(t *testing.T) {
		ibgu := newIBGU()
		ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-10 * time.Minute))
		reconciler := setup(ibgu)

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, string(utils.ConditionReasons.NoEligibleClusters), succeeded.Reason)
	})
}

// TestReferenceGateTransientThenTerminal verifies that an unresolvable reference is
// transient within the grace window (handling GitOps sync lag) and terminal after it.
func TestReferenceGateTransientThenTerminal(t *testing.T) {
	newIBGU := func() *ibguv1alpha1.ImageBasedGroupUpgrade {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc-missing"}}
		return ibgu
	}
	setup := func() IBGUReconciler {
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
		// A compatible seed reader keeps the seed gate out of the way.
		return IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}}
	}

	t.Run("within grace: retries", func(t *testing.T) {
		ibgu := newIBGU()
		ibgu.CreationTimestamp = metav1.Now()
		reconciler := setup()

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked)
		assert.Nil(t, succeededCondition(ibgu))
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), progressingCondition(ibgu).Reason)
	})

	t.Run("past grace: terminal", func(t *testing.T) {
		ibgu := newIBGU()
		ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-10 * time.Minute))
		reconciler := setup()

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), succeeded.Reason)
	})
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

			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)
			assert.False(t, blocked)

			// No CGU is created for either plan item.
			for _, name := range []string{"name-prep-0", "name-upgrade-1"} {
				cgu := &v1alpha1.ClusterGroupUpgrade{}
				assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
					types.NamespacedName{Name: name, Namespace: "namespace"}, cgu)))
			}
		})
	}
}

// TestCompletedEmptyIBGUMigratesToTerminal verifies the one-time migration of an
// IBGU that completed under older code with no CGU (Progressing=Completed, no
// Succeeded condition): it is migrated to a terminal block (Succeeded=False,
// NoEligibleClusters) rather than revived when a cluster later matches.
func TestCompletedEmptyIBGUMigratesToTerminal(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.Progressing, utils.ConditionReasons.Completed,
		metav1.ConditionFalse, "All plan steps are completed")

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// A cluster that now matches the selector appears after completion.
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("late", map[string]string{"common": "true"})))

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
		Recorder: record.NewFakeRecorder(10)}

	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
	assert.Equal(t, string(utils.ConditionReasons.NoEligibleClusters), succeeded.Reason)

	// A second reconcile short-circuits on the Succeeded presence and creates no CGU.
	blocked, err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)))
}

// TestCompletedIBGUMigratesToSucceeded verifies that an IBGU that completed under
// older code with CGUs migrates to Succeeded=True (Completed) when its plan ends in
// a finalizing action, or Succeeded=False (CompletedWithFailures) when a cluster
// recorded a failed action. A completed non-finalizing tail instead stays resumable
// (see TestCompletedNonFinalizingTailStaysResumable).
func TestCompletedIBGUMigratesToSucceeded(t *testing.T) {
	t.Run("clean completion of a finalizing plan migrates to Succeeded=True", func(t *testing.T) {
		ibgu := newIBGUForResolution(
			[]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.FinalizeUpgrade}}})
		utils.SetStatusCondition(&ibgu.Status.Conditions,
			utils.ConditionTypes.Progressing, utils.ConditionReasons.Completed,
			metav1.ConditionFalse, "All plan steps are completed")

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// A completed plan-item-0 CGU for the finalizing plan item.
		finalizeCGU := v1alpha1.ClusterGroupUpgrade{
			ObjectMeta: v1.ObjectMeta{
				Name:      "name-finalizeupgrade-0",
				Namespace: "namespace",
				Labels:    map[string]string{utils.CGUOwnerIBGULabel: "name"},
			},
			Spec: v1alpha1.ClusterGroupUpgradeSpec{
				Clusters:              []string{"cluster1"},
				ManifestWorkTemplates: []string{"name-finalizeupgrade"},
			},
			Status: v1alpha1.ClusterGroupUpgradeStatus{
				Conditions: []v1.Condition{{Type: string(utils.ConditionTypes.Succeeded)}},
			},
		}
		assert.NoError(t, fakeClient.Create(context.TODO(), &finalizeCGU))

		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionTrue, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.Completed), succeeded.Reason)
	})

	t.Run("completion with a failed action migrates to CompletedWithFailures", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		utils.SetStatusCondition(&ibgu.Status.Conditions,
			utils.ConditionTypes.Progressing, utils.ConditionReasons.Completed,
			metav1.ConditionFalse, "All plan steps are completed")
		ibgu.Status.Clusters = []ibguv1alpha1.ClusterState{
			{Name: "cluster1", FailedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Prep}}},
		}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		prepCGU := completedPrepCGU([]string{"cluster1"})
		assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))

		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.CompletedWithFailures), succeeded.Reason)
	})
}

// TestCompletedNonFinalizingTailStaysResumable verifies the incremental-append
// workflow: a completed non-finalizing plan (Progressing=Completed, no Succeeded,
// with CGUs) is not stamped terminal, and an appended Upgrade item resumes it by
// creating the next CGU.
func TestCompletedNonFinalizingTailStaysResumable(t *testing.T) {
	// A completed Prep step; the operator has appended an Upgrade item.
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})
	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.Progressing, utils.ConditionReasons.Completed,
		metav1.ConditionFalse, "All plan steps are completed")

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
	// The cluster has completed prep, so it is eligible for the appended Upgrade.
	assert.NoError(t, fakeClient.Create(context.TODO(), managedClusterWithLabels("cluster1",
		map[string]string{"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

	// The IBGU is not stamped terminal: the appended item can still run.
	assert.Nil(t, succeededCondition(ibgu))
	// The Upgrade CGU for the appended item is created.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
}

// TestCompletedWithFailuresAtEndOfPlan verifies that a plan running to the end with
// a cluster that recorded a failed action reports Succeeded=False, reason
// CompletedWithFailures (a clean run with a finalizing tail reports Succeeded=True,
// Completed).
func TestCompletedWithFailuresAtEndOfPlan(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	// A completed plan-item-0 CGU exists (plan runs to the end), and one cluster
	// recorded a failed action in the IBGU status.
	ibgu.Status.Clusters = []ibguv1alpha1.ClusterState{
		{Name: "cluster1",
			FailedActions: []ibguv1alpha1.ActionMessage{{Action: ibguv1alpha1.Prep, Message: "failed prep"}}},
	}

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	prepCGU := completedPrepCGU([]string{"cluster1"})
	assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
	assert.Equal(t, string(utils.ConditionReasons.CompletedWithFailures), succeeded.Reason)
}

// TestItem0EmptyAtCGUCreationIsBlock verifies CodeRabbit finding #3: an empty
// selection discovered at first-CGU creation for plan item 0 is signalled as the
// empty-set condition (planItemEmptyAtZero) rather than reported as a completion,
// while a later plan item that legitimately narrows to zero is a completion.
func TestItem0EmptyAtCGUCreationIsBlock(t *testing.T) {
	t.Run("plan item 0 empty is the empty-set block", func(t *testing.T) {
		// Plan item 0 is Prep but no ManagedCluster exists, so resolution is empty.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}

		cguList := &v1alpha1.ClusterGroupUpgradeList{}
		result, err := reconciler.ensureCGUForPlanItem(context.TODO(), ibgu, &ibgu.Spec.Plan[0], 0, cguList)
		assert.NoError(t, err)
		assert.Equal(t, planItemEmptyAtZero, result)
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
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}

		cguList := &v1alpha1.ClusterGroupUpgradeList{}
		assert.NoError(t, reconciler.List(context.TODO(), cguList))
		result, err := reconciler.ensureCGUForPlanItem(context.TODO(), ibgu, &ibgu.Spec.Plan[1], 1, cguList)
		assert.NoError(t, err)
		assert.Equal(t, planItemCompleted, result)
	})
}

// TestPreflightWindowExpiredOnPassPath verifies CodeRabbit finding #2 (per the
// Developer of Record's direction): when every pre-flight gate passes but the grace
// window since creation has already expired before the first CGU is created (e.g.
// TALM was down past the window and recovered), the IBGU is blocked terminally with
// reason PreflightWindowExpired and no CGU is created.
func TestPreflightWindowExpiredOnPassPath(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-10 * time.Minute))

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// A cluster eligible for Prep so all gates pass.
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
		Recorder: record.NewFakeRecorder(10)}

	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

	succeeded := succeededCondition(ibgu)
	assert.NotNil(t, succeeded)
	assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
	assert.Equal(t, string(utils.ConditionReasons.PreflightWindowExpired), succeeded.Reason)
	// No CGU was created.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)))
}

// TestAppendResumesCompletion verifies the Developer of Record's append design: a
// completed IBGU whose completion was recorded against an earlier generation resumes
// when a plan item is appended (the generation bumps). The Succeeded condition is
// dropped, the appended item's CGU is created on the frozen cluster set, and the
// pre-flight gates do not run again.
func TestAppendResumesCompletion(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Prep}},
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})
	// The spec was edited to append the Upgrade item, so the generation is now 2 while
	// the completion was recorded at generation 1.
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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

	// The completion Succeeded condition is dropped so the IBGU resumes.
	assert.Nil(t, succeededCondition(ibgu))
	assert.Equal(t, string(utils.ConditionReasons.InProgress), progressingCondition(ibgu).Reason)
	// The appended Upgrade CGU is created on the frozen cluster set from plan item 0.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
}

// TestCompletedWithFailuresResumesOnAppend verifies that a CompletedWithFailures
// IBGU also resumes when a recovery item (AbortOnFailure) is appended.
func TestCompletedWithFailuresResumesOnAppend(t *testing.T) {
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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

	assert.Nil(t, succeededCondition(ibgu))
	// The appended AbortOnFailure CGU is created.
	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-abortonfailure-1", Namespace: "namespace"}, cgu))
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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

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

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.False(t, blocked)

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
	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
		SeedImageReader: reader}

	blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.True(t, blocked)

	cond := progressingCondition(ibgu)
	assert.NotNil(t, cond)
	assert.Equal(t, string(utils.ConditionReasons.NoClustersMatchSelector), cond.Reason)
	assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
	// The registry was never read: the empty-set gate short-circuited before the seed gate.
	assert.False(t, reader.called)
}

// TestEnsureManifestsEmptyIBGUDoesNotComplete verifies that an item-0-empty IBGU
// never reports success: within grace it retries (no Succeeded), and past grace it
// is Succeeded=False — never Progressing=Completed or Succeeded=True.
func TestEnsureManifestsEmptyIBGUDoesNotComplete(t *testing.T) {
	newEmptyIBGU := func() *ibguv1alpha1.ImageBasedGroupUpgrade {
		return newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	}

	t.Run("within grace does not complete", func(t *testing.T) {
		ibgu := newEmptyIBGU()
		ibgu.CreationTimestamp = metav1.Now()
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}

		_, err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.Nil(t, succeededCondition(ibgu))
		assert.NotEqual(t, string(utils.ConditionReasons.Completed), progressingCondition(ibgu).Reason)
	})

	t.Run("past grace is terminal, never Succeeded=True", func(t *testing.T) {
		ibgu := newEmptyIBGU()
		ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-10 * time.Minute))
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder: record.NewFakeRecorder(10)}

		_, err = reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.NotEqual(t, string(utils.ConditionReasons.Completed), succeeded.Reason)
	})
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

			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			missing, err := reconciler.resolvePrepReferences(context.TODO(), ibgu)
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
// gate: a Prep plan with an unresolvable reference is, within the grace window,
// retried (transient) before any CGU is created, while resolvable references,
// Upgrade-only plans, and already-in-progress IBGUs are not blocked. The
// transient-then-terminal arc is covered by TestReferenceGateTransientThenTerminal.
func TestEnsureManifestsPrepReferenceGate(t *testing.T) {
	t.Run("prep plan with missing reference is retried within grace and creates no CGU", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc-missing"}}
		ibgu.CreationTimestamp = metav1.Now() // within the grace window

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		// A compatible seed reader is injected so the seed-format gate passes and
		// this test continues to exercise the reference gate.
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		// Transient within grace: requeue, no terminal state.
		assert.True(t, blocked)
		assert.Nil(t, succeededCondition(ibgu))

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
		assert.Contains(t, cond.Message, `BackupContent "bc-missing"`)

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
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.NoError(t, err)
	})

	t.Run("prep plan with missing oadpContent configmap is retried within grace and creates no CGU", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
		ibgu.Spec.IBUSpec.OADPContent = []lcav1.ConfigMapRef{{Name: "cm-missing", Namespace: "ns1"}}
		ibgu.CreationTimestamp = metav1.Now() // within the grace window

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked)
		assert.Nil(t, succeededCondition(ibgu))

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
		assert.Contains(t, cond.Message, `ConfigMap "ns1/cm-missing"`)

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
		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)

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

		reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
	})
}
