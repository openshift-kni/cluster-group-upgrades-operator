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
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClusterGroupUpgradeReconciler_getClusterComplianceWithPolicy(t *testing.T) {
	type fields struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
	}
	type args struct {
		clusterName string
		policy      *unstructured.Unstructured
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "compliant",
			args: args{
				clusterName: "cluster1",
				policy: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"status": map[string]interface{}{
							"status": []interface{}{
								map[string]interface{}{
									"clustername": "cluster1",
									"compliant":   "Compliant",
								},
							},
						},
					},
				},
			},
			want: utils.ClusterStatusCompliant,
		},

		{
			name: "non-compliant",
			args: args{
				clusterName: "cluster1",
				policy: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"status": map[string]interface{}{
							"status": []interface{}{
								map[string]interface{}{
									"clustername": "cluster1",
									"compliant":   "NonCompliant",
								},
							},
						},
					},
				},
			},
			want: utils.ClusterStatusNonCompliant,
		},

		{
			name: "pending",
			args: args{
				clusterName: "cluster1",
				policy: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"status": map[string]interface{}{
							"status": []interface{}{
								map[string]interface{}{
									"clustername": "cluster1",
									"compliant":   "Pending",
								},
							},
						},
					},
				},
			},
			want: utils.ClusterStatusNonCompliant,
		},

		{
			name: "no compliant status",
			args: args{
				clusterName: "cluster1",
				policy: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"status": map[string]interface{}{
							"status": []interface{}{
								map[string]interface{}{
									"clustername": "cluster1",
								},
							},
						},
					},
				},
			},
			want: utils.ClusterStatusNonCompliant,
		},

		{
			name: "no cluster entry",
			args: args{
				clusterName: "cluster1",
				policy: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"status": map[string]interface{}{
							"status": []interface{}{},
						},
					},
				},
			},
			want: utils.ClusterNotMatchedWithPolicy,
		},
	}

	fakeClient, err := getFakeClientFromObjects([]client.Object{}...)
	if err != nil {
		t.Errorf("error in creating fake client")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client: fakeClient,
				Log:    logr.Discard(),
				Scheme: testscheme,
				//Recorder:
			}
			if got := r.getClusterComplianceWithPolicy(tt.args.clusterName, tt.args.policy); got != tt.want {
				t.Errorf("ClusterGroupUpgradeReconciler.getClusterComplianceWithPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterGroupUpgradeReconciler_getCGUControllerWorkerCount(t *testing.T) {
	tests := []struct {
		name        string
		envVarValue string
		wantCount   int
	}{
		{
			name:        "good value",
			envVarValue: "10",
			wantCount:   10,
		},
		{
			name:        "negative value",
			envVarValue: "-1",
			wantCount:   5,
		},
		{
			name:        "zero",
			envVarValue: "0",
			wantCount:   5,
		},
		{
			name:        "non numeric",
			envVarValue: "abc",
			wantCount:   5,
		},
	}

	r := &ClusterGroupUpgradeReconciler{
		Log: logr.Discard(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(utils.CGUControllerWorkerCountEnv, tt.envVarValue)
			if gotCount := r.getCGUControllerWorkerCount(); gotCount != tt.wantCount {
				t.Errorf("ClusterGroupUpgradeReconciler.getCGUControllerWorkerCount() = %v, want %v", gotCount, tt.wantCount)
			}
		})
	}
}
