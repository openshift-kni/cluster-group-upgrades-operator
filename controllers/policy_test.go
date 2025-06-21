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

func TestGetNextNonCompliantPolicyForCluster(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                     string
		clusterGroupUpgrade      *ranv1alpha1.ClusterGroupUpgrade
		clusterName              string
		startIndex               int
		getPolicyByNameFunc      func(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error)
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
			getPolicyByNameFunc: func(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error) {
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
			expectedIndex:   0, // Should break on first non-compliant policy regardless of batch status
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
			reconciler := &ClusterGroupUpgradeReconciler{
				Log: logr.Discard(),
			}

			// Create dependencies for testing
			deps := &PolicyEvaluationDeps{
				GetPolicy:     tt.getPolicyByNameFunc,
				GetCompliance: tt.getClusterComplianceFunc,
			}

			// Use default policy function if not provided
			if deps.GetPolicy == nil {
				deps.GetPolicy = func(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error) {
					return &unstructured.Unstructured{
						Object: map[string]interface{}{
							"metadata": map[string]interface{}{
								"name":      name,
								"namespace": namespace,
							},
						},
					}, nil
				}
			}

			// Use default compliance function if not provided
			if deps.GetCompliance == nil {
				deps.GetCompliance = func(clusterName string, policy *unstructured.Unstructured) string {
					return utils.ClusterStatusNonCompliant
				}
			}

			index, isSoaking, err := reconciler.getNextNonCompliantPolicyForCluster(
				ctx, tt.clusterGroupUpgrade, tt.clusterName, tt.startIndex, deps)

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
			expectedIndex:               0,
			expectedSoaking:             true,
			expectedError:               false,
			expectFirstCompliantAtSet:   false, // Already set, shouldn't change
			expectFirstCompliantAtReset: false,
		},
		{
			name: "cluster compliant but should NOT soak - resets FirstCompliantAt",
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
				return false, nil // Should NOT soak
			},
			expectedIndex:               1,
			expectedSoaking:             false,
			expectedError:               false,
			expectFirstCompliantAtSet:   false,
			expectFirstCompliantAtReset: true,
		},
		{
			name: "ShouldSoak returns error - continues to next policy",
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
				if policy.GetName() == "policy1" {
					return false, errors.New("soaking evaluation error")
				}
				return false, nil
			},
			expectedIndex:   1,
			expectedSoaking: false,
			expectedError:   false,
		},
		{
			name: "multiple policies with early soaking termination",
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
				switch policy.GetName() {
				case "policy1":
					return false, nil // Should NOT soak, continues
				case "policy2":
					return true, nil // Should soak, terminates here
				case "policy3":
					return true, nil // Shouldn't reach this
				}
				return false, nil
			},
			expectedIndex:             1,
			expectedSoaking:           true,
			expectedError:             false,
			expectFirstCompliantAtSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &ClusterGroupUpgradeReconciler{
				Log: logr.Discard(),
			}

			// Store original FirstCompliantAt values for comparison
			originalTime := tt.clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[tt.clusterName].FirstCompliantAt

			// Create dependencies for testing
			deps := &PolicyEvaluationDeps{
				GetPolicy: func(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error) {
					return &unstructured.Unstructured{
						Object: map[string]interface{}{
							"metadata": map[string]interface{}{
								"name":      name,
								"namespace": namespace,
							},
						},
					}, nil
				},
				GetCompliance: tt.getClusterComplianceFunc,
				ShouldSoak:    tt.shouldSoakFunc,
			}

			index, isSoaking, err := reconciler.getNextNonCompliantPolicyForCluster(
				ctx, tt.clusterGroupUpgrade, tt.clusterName, tt.startIndex, deps)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedIndex, index, "Policy index should match expected")
			assert.Equal(t, tt.expectedSoaking, isSoaking, "Soaking status should match expected")

			// Check FirstCompliantAt behavior
			newTime := tt.clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[tt.clusterName].FirstCompliantAt

			if tt.expectFirstCompliantAtSet {
				assert.False(t, newTime.IsZero(), "FirstCompliantAt should be set")
				if originalTime.IsZero() {
					assert.True(t, newTime.After(time.Now().Add(-5*time.Second)), "FirstCompliantAt should be recent")
				}
			}

			if tt.expectFirstCompliantAtReset {
				assert.True(t, newTime.IsZero(), "FirstCompliantAt should be reset to zero")
			}
		})
	}
}

func TestGetNextNonCompliantPolicyForCluster_EdgeCases(t *testing.T) {
	ctx := context.Background()
	reconciler := &ClusterGroupUpgradeReconciler{
		Log: logr.Discard(),
	}

	t.Run("partial dependencies provided", func(t *testing.T) {
		cgu := &ranv1alpha1.ClusterGroupUpgrade{
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
		}

		deps := &PolicyEvaluationDeps{
			GetPolicy: func(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error) {
				return &unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      name,
							"namespace": namespace,
						},
					},
				}, nil
			},
			GetCompliance: func(clusterName string, policy *unstructured.Unstructured) string {
				return utils.ClusterStatusNonCompliant
			},
			// ShouldSoak is nil, should use default
		}

		index, isSoaking, err := reconciler.getNextNonCompliantPolicyForCluster(
			ctx, cgu, "cluster1", 0, deps)

		assert.NoError(t, err)
		assert.Equal(t, 0, index) // First policy should be non-compliant
		assert.False(t, isSoaking)
	})
}
