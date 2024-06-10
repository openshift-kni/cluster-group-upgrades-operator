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
	lcav1alpha1 "github.com/openshift-kni/lifecycle-agent/api/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	namePrep := "name-prep"
	nameFinalize := "name-finalize"
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
								Name:  "spoke1",
								State: "timedout",
							},
						},
					},
				},
			},
			expectedClustersStatus: []ibguv1alpha1.ClusterState{
				{
					Name:  "spoke1",
					State: "timedout",
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
					Name:  "spoke1",
					State: "complete",
				},
			},
		},
		{
			name: "two CGUs, one in progress",
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
					Name:          "spoke1",
					State:         "InProgress",
					CurrentAction: &nameFinalize,
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
					Name:  "spoke1",
					State: "complete",
				},
				{
					Name:  "spoke4",
					State: "complete",
				},
				{
					Name:          "spoke6",
					State:         "InProgress",
					CurrentAction: &namePrep,
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

			RolloutStrategy: ibguv1alpha1.RolloutStrategy{
				Timeout:        50,
				MaxConcurrency: 2,
			},
			ClusterLabelSelectors: []v1.LabelSelector{

				{
					MatchLabels: map[string]string{
						"common": "true",
					},
				},
			},
			IBUSpec: lcav1alpha1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1alpha1.SeedImageRef{
					Version: "version",
					Image:   "image",
				},
			},
		},
	}

	for _, test := range tests {
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
	}
}

func TestEnsureManifests(t *testing.T) {
	tests := []struct {
		name         string
		actions      []ibguv1alpha1.ImageBasedUpgradeAction
		expectedMWRS []string
		CGUs         []v1alpha1.ClusterGroupUpgrade
		expectedCGUs []v1alpha1.ClusterGroupUpgrade
	}{
		{
			name: "without append",
			actions: []ibguv1alpha1.ImageBasedUpgradeAction{
				ibguv1alpha1.Prep,
				ibguv1alpha1.Upgrade,
				ibguv1alpha1.Finalize,
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
			actions: []ibguv1alpha1.ImageBasedUpgradeAction{
				ibguv1alpha1.Prep,
				ibguv1alpha1.Upgrade,
				ibguv1alpha1.Finalize,
			},
			expectedMWRS: []string{"name-prep", "name-upgrade", "name-finalize"},
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
		ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
			ObjectMeta: v1.ObjectMeta{
				Name:      "name",
				Namespace: "namespace",
			},
			Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{

				RolloutStrategy: ibguv1alpha1.RolloutStrategy{
					Timeout:        50,
					MaxConcurrency: 2,
				},
				ClusterLabelSelectors: []v1.LabelSelector{

					{
						MatchLabels: map[string]string{
							"common": "true",
						},
					},
				},
				Actions: test.actions,
				IBUSpec: lcav1alpha1.ImageBasedUpgradeSpec{
					SeedImageRef: lcav1alpha1.SeedImageRef{
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
			assert.Equal(t, expected.Spec.BlockingCRs, cgu.Spec.BlockingCRs)
			assert.NoError(t, err)
		}
	}
}
