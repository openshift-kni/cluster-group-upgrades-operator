package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

func init() {
	testscheme.AddKnownTypes(ibguv1alpha1.SchemeGroupVersion, &ibguv1alpha1.ImageBasedGroupUpgrade{})
	testscheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ClusterGroupUpgradeList{})
	testscheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ClusterGroupUpgrade{})
	testscheme.AddKnownTypes(mwv1alpha1.GroupVersion, &mwv1alpha1.ManifestWorkReplicaSet{})
	testscheme.AddKnownTypes(mwv1alpha1.GroupVersion, &mwv1alpha1.ManifestWorkReplicaSetList{})
}

func TestSyncStatusWithCGUs(t *testing.T) {
	errorMsg := "error message"
	cmw := &v1alpha1.ManifestWorkStatus{
		Name: "ibu-finalize",
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
	one := 1
	tests := []struct {
		name                   string
		CGUs                   []v1alpha1.ClusterGroupUpgrade
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
						Name:      "name-finalize",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-finalize"},
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
						{Action: ibguv1alpha1.Finalize, Message: "error message"},
					},
				},
			},
		},
		{
			name: "two CGUs with reverse order",
			CGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-finalize",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-finalize"},
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
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name: "spoke1",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade}, {Action: ibguv1alpha1.Finalize},
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
						Name:      "name-finalize",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-finalize"},
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
					CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.Finalize},
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
						ManifestWorkTemplates: []string{"ibu-permissions", "name-prep"},
					},
					Status: v1alpha1.ClusterGroupUpgradeStatus{
						Status: v1alpha1.UpgradeStatus{
							CurrentBatchRemediationProgress: map[string]*v1alpha1.ClusterRemediationProgress{
								"spoke6": {
									ManifestWorkIndex: &one,
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
						Name:      "name-prep-upgrade-finalize",
						Namespace: "namespace",
						Labels: map[string]string{
							utils.CGUOwnerIBGULabel: "name",
						},
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{"name-prep", "name-upgrade", "name-finalize"},
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
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade}, {Action: ibguv1alpha1.Finalize},
					},
				},
				{
					Name: "spoke4",
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade}, {Action: ibguv1alpha1.Finalize},
					},
				},
				{
					Name:          "spoke6",
					CurrentAction: &ibguv1alpha1.ActionMessage{Action: ibguv1alpha1.Finalize},
					CompletedActions: []ibguv1alpha1.ActionMessage{
						{Action: ibguv1alpha1.Prep}, {Action: ibguv1alpha1.Upgrade},
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
			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			for _, cgu := range test.CGUs {
				err := fakeClient.Create(context.TODO(), &cgu)
				if err != nil {
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

func TestGetConfigMapManifests(t *testing.T) {
	tests := []struct {
		name         string
		cm           corev1.ConfigMap
		expectedJson string
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
			expectedJson: `{"kind":"ConfigMap","apiVersion":"v1","metadata":{"name":"name","namespace":"namespace","creationTimestamp":null},"data":{"klusterlet.yaml":"data"}}`,
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
			err = fakeClient.Create(context.TODO(), &test.cm)
			if err != nil {
				panic(err)
			}
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
			manifests := reconciler.getConfigMapManifests(context.TODO(), ibgu)
			assert.Equal(t, 1, len(manifests))
			assert.NoError(t, err)
			assert.JSONEq(t, test.expectedJson, string(manifests[0].Raw))
		})
	}
}

func TestEnsureClusterLabels(t *testing.T) {
	tests := []struct {
		name                    string
		managedClusters         []clusterv1.ManagedCluster
		expectedManagedClusters []clusterv1.ManagedCluster
		clusterStates           []ibguv1alpha1.ClusterState
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
							"lcm.openshift.io/ibgu-name-Abort":   "failed",
							"lcm.openshift.io/ibgu-name-Upgrade": "failed",
							"lcm.openshift.io/ibgu-name-Prep":    "failed",
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
							"lcm.openshift.io/ibgu-name-Prep": "failed",
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
							"lcm.openshift.io/ibgu-name-Prep":    "completed",
							"lcm.openshift.io/ibgu-name-Upgrade": "completed",
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
							Action: ibguv1alpha1.Finalize,
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
							"lcm.openshift.io/ibgu-name-Prep": "completed",
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
			assert.NoError(t, err)
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
		name         string
		plan         []ibguv1alpha1.PlanItem
		expectedMWRS []string
		CGUs         []v1alpha1.ClusterGroupUpgrade
		expectedCGUs []v1alpha1.ClusterGroupUpgrade
	}{
		{
			name: "without append",
			plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{ibguv1alpha1.Prep, ibguv1alpha1.Upgrade, ibguv1alpha1.Finalize},
				},
			},
			expectedMWRS: []string{"name-prep", "name-upgrade", "name-finalize"},
			expectedCGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "name-prep-upgrade-finalize",
					},
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
					Actions: []string{ibguv1alpha1.Upgrade, ibguv1alpha1.Finalize},
				},
			},
			expectedMWRS: []string{"name-upgrade", "name-finalize"},
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
						ManifestWorkTemplates: []string{
							"name-prep",
						},
					},
				},
			},
			expectedCGUs: []v1alpha1.ClusterGroupUpgrade{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-prep",
						Namespace: "namespace",
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{
							"name-ibu-permissions",
							"name-prep",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "name-upgrade-finalize",
						Namespace: "namespace",
					},
					Spec: v1alpha1.ClusterGroupUpgradeSpec{
						ManifestWorkTemplates: []string{
							"name-upgrade",
							"name-finalize",
						},
						BlockingCRs: []v1alpha1.BlockingCR{
							{
								Name:      "name-prep",
								Namespace: "namespace",
							},
						},
					},
				},
			},
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
			if err != nil {
				t.Errorf("error in creating fake client")
			}
			reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}

			err = reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)
			list := &mwv1alpha1.ManifestWorkReplicaSetList{}
			err = reconciler.List(context.TODO(), list)
			assert.NoError(t, err)
			mwrsNames := []string{}
			for _, mwrs := range list.Items {
				mwrsNames = append(mwrsNames, mwrs.Name)
			}
			for _, expected := range test.expectedMWRS {
				assert.Contains(t, mwrsNames, expected)
			}

			cgu := &v1alpha1.ClusterGroupUpgrade{}
			for _, expected := range test.expectedCGUs {
				err = reconciler.Get(context.TODO(), types.NamespacedName{Name: expected.Name, Namespace: "namespace"}, cgu)
				assert.NoError(t, err)
				assert.Equal(t, expected.Spec.BlockingCRs, cgu.Spec.BlockingCRs)
			}
		})
	}
}
