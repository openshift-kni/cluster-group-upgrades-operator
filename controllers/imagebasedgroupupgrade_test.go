package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
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
	testscheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ClusterGroupUpgradeList{})
	testscheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ClusterGroupUpgrade{})
	testscheme.AddKnownTypes(mwv1alpha1.SchemeGroupVersion, &mwv1alpha1.ManifestWorkReplicaSet{})
	testscheme.AddKnownTypes(mwv1alpha1.SchemeGroupVersion, &mwv1alpha1.ManifestWorkReplicaSetList{})
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
			expectedJson: `{"kind":"ConfigMap","apiVersion":"v1","metadata":{"name":"name","namespace":"namespace"},"data":{"klusterlet.yaml":"data"}}`,
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
			expectedMWRS: []string{"name-prep", "name-upgrade", "name-finalizeupgrade"},
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
			expectedMWRS: []string{"name-abort"},
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
			name: "all completed",
			plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{ibguv1alpha1.Prep},
				},
			},
			expectedMWRS: []string{},
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
			expectedMWRS: []string{"name-upgrade", "name-finalizeupgrade"},
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
	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}
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
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	cgu := &v1alpha1.ClusterGroupUpgrade{}
	assert.NoError(t, reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-1", Namespace: "namespace"}, cgu))
	assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
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

// TestEnsureManifestsItem0ZeroIsFinalAfterCompletion verifies that once the IBGU
// has reported completion while plan item 0 created no CGU (it resolved to zero
// eligible clusters), a ManagedCluster that later matches the selector does not
// restart the finished, empty rollout: plan item 0's zero result is final.
func TestEnsureManifestsItem0ZeroIsFinalAfterCompletion(t *testing.T) {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
		{Actions: []string{ibguv1alpha1.Upgrade}},
	})
	// IBGU already reported completion; no plan item 0 CGU exists.
	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.Progressing, utils.ConditionReasons.Completed,
		metav1.ConditionFalse, "All plan steps are completed")

	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	// A cluster that now matches the selector and the Upgrade predicate appears
	// after completion; it must not pull plan item 0 back into a rollout.
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("late", map[string]string{
			"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))

	reconciler := IBGUReconciler{Client: fakeClient, Scheme: testscheme, Log: logr.Discard()}
	err = reconciler.ensureManifests(context.TODO(), ibgu)
	assert.NoError(t, err)

	cgu := &v1alpha1.ClusterGroupUpgrade{}
	err = reconciler.Get(context.TODO(),
		types.NamespacedName{Name: "name-upgrade-0", Namespace: "namespace"}, cgu)
	assert.True(t, apierrors.IsNotFound(err))
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
