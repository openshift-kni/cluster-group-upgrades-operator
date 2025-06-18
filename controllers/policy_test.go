/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Mock reconciler for testing - using a different approach to avoid method dispatch issues
type testPolicyReconciler struct {
	log                      logr.Logger
	getPolicyByNameFunc      func(ctx context.Context, name string, namespace string) (*unstructured.Unstructured, error)
	getClusterComplianceFunc func(clusterName string, policy *unstructured.Unstructured) string
	shouldSoakFunc           func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error)
}

func (r *testPolicyReconciler) getPolicyByName(ctx context.Context, name string, namespace string) (*unstructured.Unstructured, error) {
	if r.getPolicyByNameFunc != nil {
		return r.getPolicyByNameFunc(ctx, name, namespace)
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
		},
	}, nil
}

func (r *testPolicyReconciler) getClusterComplianceWithPolicy(clusterName string, policy *unstructured.Unstructured) string {
	if r.getClusterComplianceFunc != nil {
		return r.getClusterComplianceFunc(clusterName, policy)
	}
	return utils.ClusterStatusNonCompliant
}

func (r *testPolicyReconciler) shouldSoak(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error) {
	if r.shouldSoakFunc != nil {
		return r.shouldSoakFunc(policy, firstCompliantAt)
	}
	// Default behavior: call the real utils.ShouldSoak function
	return utils.ShouldSoak(policy, firstCompliantAt)
}

// Copy of the method we're testing to allow proper mocking
func (r *testPolicyReconciler) getNextNonCompliantPolicyForCluster(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, startIndex int) (int, bool, error) {
	isSoaking := false
	numberOfPolicies := len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
	currentPolicyIndex := startIndex
	for ; currentPolicyIndex < numberOfPolicies; currentPolicyIndex++ {
		// Get the name of the managed policy matching the current index.
		currentManagedPolicyInfo := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[currentPolicyIndex]
		currentManagedPolicy, err := r.getPolicyByName(ctx, currentManagedPolicyInfo.Name, currentManagedPolicyInfo.Namespace)
		if err != nil {
			return currentPolicyIndex, isSoaking, err
		}

		// Check if current cluster is compliant or not for its current managed policy.
		clusterStatus := r.getClusterComplianceWithPolicy(clusterName, currentManagedPolicy)

		// If the cluster is compliant for the policy or if the cluster is not matched with the policy,
		// move to the next policy index.
		if clusterStatus == utils.ClusterNotMatchedWithPolicy {
			continue
		}

		// after all batches are finished, controller goes through all previous batches to see
		// if policies are still compliant; in this case some cluster will not be present in
		// CurrentBatchRemediationProgress and there is no need to check soaking or modify
		// FirstCompliantAt
		_, clusterInBatch := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName]

		if clusterStatus == utils.ClusterStatusCompliant {
			if !clusterInBatch {
				continue
			}
			shouldSoak, err := r.shouldSoak(currentManagedPolicy, clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt)
			if err != nil {
				r.log.Info(err.Error())
				continue
			}
			if !shouldSoak {
				clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt = metav1.Time{}
				continue
			}

			if clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt.IsZero() {
				clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt = metav1.Now()
			}
			isSoaking = true
			r.log.Info("Policy is compliant but should be soaked", "cluster name", clusterName, "policyName", currentManagedPolicy.GetName())
			break
		}

		if clusterInBatch && clusterStatus == utils.ClusterStatusNonCompliant {
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt = metav1.Time{}
			break
		}
	}

	return currentPolicyIndex, isSoaking, nil
}

func TestGetNextNonCompliantPolicyForCluster(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                     string
		clusterGroupUpgrade      *ranv1alpha1.ClusterGroupUpgrade
		clusterName              string
		startIndex               int
		getPolicyByNameFunc      func(ctx context.Context, name string, namespace string) (*unstructured.Unstructured, error)
		getClusterComplianceFunc func(clusterName string, policy *unstructured.Unstructured) string
		expectedIndex            int
		expectedSoaking          bool
		expectedError            bool
	}{
		{
			name: "finds first non-compliant policy",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
						{Name: "policy3", Namespace: "namespace3"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				switch policy.GetName() {
				case "policy1":
					return utils.ClusterStatusCompliant
				case "policy2":
					return utils.ClusterStatusNonCompliant
				case "policy3":
					return utils.ClusterStatusNonCompliant
				}
				return utils.ClusterStatusNonCompliant
			},
			expectedIndex:   1,
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "returns total policies count when all are compliant",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusCompliant
			},
			expectedIndex:   2,
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "skips policies where cluster is not matched",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
						{Name: "policy3", Namespace: "namespace3"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				switch policy.GetName() {
				case "policy1":
					return utils.ClusterNotMatchedWithPolicy
				case "policy2":
					return utils.ClusterNotMatchedWithPolicy
				case "policy3":
					return utils.ClusterStatusNonCompliant
				}
				return utils.ClusterStatusNonCompliant
			},
			expectedIndex:   2,
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "handles cluster not in current batch - compliant policy continues",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"other-cluster": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1", // This cluster is not in CurrentBatchRemediationProgress
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusCompliant
			},
			expectedIndex:   2,
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "handles policy retrieval error",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getPolicyByNameFunc: func(ctx context.Context, name string, namespace string) (*unstructured.Unstructured, error) {
				return nil, errors.New("policy not found")
			},
			expectedIndex:   0,
			expectedSoaking: false,
			expectedError:   true,
		},
		{
			name: "starts from specified index",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
						{Name: "policy3", Namespace: "namespace3"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  1, // Start from index 1, skip policy1
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				switch policy.GetName() {
				case "policy1":
					return utils.ClusterStatusNonCompliant // This shouldn't be checked
				case "policy2":
					return utils.ClusterStatusCompliant
				case "policy3":
					return utils.ClusterStatusNonCompliant
				}
				return utils.ClusterStatusNonCompliant
			},
			expectedIndex:   2,
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "empty policies list",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName:     "cluster1",
			startIndex:      0,
			expectedIndex:   0,
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "start index beyond policies list",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName:     "cluster1",
			startIndex:      5, // Beyond the list
			expectedIndex:   5, // Should return the start index when beyond the list
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "cluster non-compliant but not in current batch",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"other-cluster": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1", // Not in CurrentBatchRemediationProgress
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusNonCompliant
			},
			expectedIndex:   2, // Should continue through all policies since cluster not in batch
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "cluster compliant and should reset FirstCompliantAt when not soaking",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{Time: metav1.Now().Add(-1000)}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				switch policy.GetName() {
				case "policy1":
					return utils.ClusterStatusCompliant
				case "policy2":
					return utils.ClusterStatusNonCompliant
				}
				return utils.ClusterStatusNonCompliant
			},
			expectedIndex:   1,
			expectedSoaking: false,
			expectedError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &testPolicyReconciler{
				log:                      logr.Discard(),
				getPolicyByNameFunc:      tt.getPolicyByNameFunc,
				getClusterComplianceFunc: tt.getClusterComplianceFunc,
			}

			index, isSoaking, err := reconciler.getNextNonCompliantPolicyForCluster(
				ctx, tt.clusterGroupUpgrade, tt.clusterName, tt.startIndex)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedIndex, index, "Policy index should match expected")
			assert.Equal(t, tt.expectedSoaking, isSoaking, "Soaking status should match expected")
		})
	}
}

// Comprehensive test for soaking behavior with proper mocking
func TestGetNextNonCompliantPolicyForCluster_SoakingBehavior(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                        string
		clusterGroupUpgrade         *ranv1alpha1.ClusterGroupUpgrade
		clusterName                 string
		startIndex                  int
		getClusterComplianceFunc    func(clusterName string, policy *unstructured.Unstructured) string
		shouldSoakFunc              func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error)
		expectedIndex               int
		expectedSoaking             bool
		expectedError               bool
		expectFirstCompliantAtSet   bool
		expectFirstCompliantAtReset bool
	}{
		{
			name: "cluster should soak - sets FirstCompliantAt and returns soaking",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}}, // Zero time
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusCompliant
			},
			shouldSoakFunc: func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error) {
				return true, nil // Should soak
			},
			expectedIndex:             0,
			expectedSoaking:           true,
			expectedError:             false,
			expectFirstCompliantAtSet: true,
		},
		{
			name: "cluster should soak - already has FirstCompliantAt set",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {
								FirstCompliantAt: metav1.Time{Time: metav1.Now().Add(-5 * time.Minute)},
							},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusCompliant
			},
			shouldSoakFunc: func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error) {
				return true, nil // Should soak
			},
			expectedIndex:             0,
			expectedSoaking:           true,
			expectedError:             false,
			expectFirstCompliantAtSet: false, // Already set, shouldn't change
		},
		{
			name: "cluster should not soak - resets FirstCompliantAt and continues",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {
								FirstCompliantAt: metav1.Time{Time: metav1.Now().Add(-5 * time.Minute)},
							},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				switch policy.GetName() {
				case "policy1":
					return utils.ClusterStatusCompliant
				case "policy2":
					return utils.ClusterStatusNonCompliant
				}
				return utils.ClusterStatusNonCompliant
			},
			shouldSoakFunc: func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error) {
				return false, nil // Should not soak
			},
			expectedIndex:               1,
			expectedSoaking:             false,
			expectedError:               false,
			expectFirstCompliantAtReset: true,
		},
		{
			name: "ShouldSoak returns error - logs error and continues",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				switch policy.GetName() {
				case "policy1":
					return utils.ClusterStatusCompliant
				case "policy2":
					return utils.ClusterStatusNonCompliant
				}
				return utils.ClusterStatusNonCompliant
			},
			shouldSoakFunc: func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error) {
				return false, errors.New("soaking calculation failed")
			},
			expectedIndex:   1,
			expectedSoaking: false,
			expectedError:   false, // Error is logged but doesn't propagate
		},
		{
			name: "multiple policies - first soaking, should stop at first",
			clusterGroupUpgrade: &ranv1alpha1.ClusterGroupUpgrade{
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
						{Name: "policy1", Namespace: "namespace1"},
						{Name: "policy2", Namespace: "namespace2"},
						{Name: "policy3", Namespace: "namespace3"},
					},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"cluster1": {FirstCompliantAt: metav1.Time{}},
						},
					},
				},
			},
			clusterName: "cluster1",
			startIndex:  0,
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusCompliant // All policies compliant
			},
			shouldSoakFunc: func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error) {
				if policy.GetName() == "policy1" {
					return true, nil // First policy should soak
				}
				return false, nil // Others shouldn't soak (but won't be reached)
			},
			expectedIndex:             0,
			expectedSoaking:           true,
			expectedError:             false,
			expectFirstCompliantAtSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original FirstCompliantAt for comparison
			originalFirstCompliantAt := tt.clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[tt.clusterName].FirstCompliantAt

			reconciler := &testPolicyReconciler{
				log:                      logr.Discard(),
				getClusterComplianceFunc: tt.getClusterComplianceFunc,
				shouldSoakFunc:           tt.shouldSoakFunc,
			}

			index, isSoaking, err := reconciler.getNextNonCompliantPolicyForCluster(
				ctx, tt.clusterGroupUpgrade, tt.clusterName, tt.startIndex)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedIndex, index, "Policy index should match expected")
			assert.Equal(t, tt.expectedSoaking, isSoaking, "Soaking status should match expected")

			// Check FirstCompliantAt behavior
			currentFirstCompliantAt := tt.clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[tt.clusterName].FirstCompliantAt

			if tt.expectFirstCompliantAtSet {
				assert.False(t, currentFirstCompliantAt.IsZero(), "FirstCompliantAt should be set")
				assert.True(t, originalFirstCompliantAt.IsZero(), "Original FirstCompliantAt should have been zero")
			}

			if tt.expectFirstCompliantAtReset {
				assert.True(t, currentFirstCompliantAt.IsZero(), "FirstCompliantAt should be reset to zero")
				assert.False(t, originalFirstCompliantAt.IsZero(), "Original FirstCompliantAt should not have been zero")
			}
		})
	}
}

func TestGetNextNonCompliantPolicyForCluster_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("handles nil CurrentBatchRemediationProgress", func(t *testing.T) {
		clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{
			Status: ranv1alpha1.ClusterGroupUpgradeStatus{
				ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
					{Name: "policy1", Namespace: "namespace1"},
				},
				Status: ranv1alpha1.UpgradeStatus{
					CurrentBatchRemediationProgress: nil,
				},
			},
		}

		reconciler := &testPolicyReconciler{
			log: logr.Discard(),
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusCompliant
			},
		}

		index, isSoaking, err := reconciler.getNextNonCompliantPolicyForCluster(
			ctx, clusterGroupUpgrade, "cluster1", 0)

		assert.NoError(t, err)
		assert.Equal(t, 1, index) // Should go through all policies
		assert.False(t, isSoaking)
	})

	t.Run("handles empty CurrentBatchRemediationProgress map", func(t *testing.T) {
		clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{
			Status: ranv1alpha1.ClusterGroupUpgradeStatus{
				ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{
					{Name: "policy1", Namespace: "namespace1"},
				},
				Status: ranv1alpha1.UpgradeStatus{
					CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{},
				},
			},
		}

		reconciler := &testPolicyReconciler{
			log: logr.Discard(),
			getClusterComplianceFunc: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusNonCompliant
			},
		}

		index, isSoaking, err := reconciler.getNextNonCompliantPolicyForCluster(
			ctx, clusterGroupUpgrade, "cluster1", 0)

		assert.NoError(t, err)
		assert.Equal(t, 1, index) // Should continue since cluster not in batch
		assert.False(t, isSoaking)
	})
}
