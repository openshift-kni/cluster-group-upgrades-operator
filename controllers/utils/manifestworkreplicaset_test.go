package utils

import (
	"testing"

	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mwv1 "open-cluster-management.io/api/work/v1"
)

var ibu = &lcav1.ImageBasedUpgrade{
	ObjectMeta: v1.ObjectMeta{
		Name: "upgrade",
	},
	Spec: lcav1.ImageBasedUpgradeSpec{
		SeedImageRef: lcav1.SeedImageRef{
			Image:   "quay.io/image/version:tag",
			Version: "14.4.0-rc.2",
		},
	},
}

func TestGetConditionMessageFromManifestWorkStatus(t *testing.T) {
	e := "some err"
	tests := []struct {
		name     string
		status   v1alpha1.ManifestWorkStatus
		expected string
	}{
		{
			name:     "failed condition",
			expected: "failed to apply",
			status: v1alpha1.ManifestWorkStatus{
				Name: "manifest",
				Status: mwv1.ManifestResourceStatus{
					Manifests: []mwv1.ManifestCondition{
						{
							Conditions: []v1.Condition{
								{
									Type:    "Applied",
									Message: "failed to apply",
									Status:  "False",
								},
							},
						},
					},
				},
			},
		},
		{
			name:     "successful apply",
			expected: "",
			status: v1alpha1.ManifestWorkStatus{
				Name: "manifest",
				Status: mwv1.ManifestResourceStatus{
					Manifests: []mwv1.ManifestCondition{
						{
							Conditions: []v1.Condition{
								{
									Type:   "Applied",
									Status: "True",
								},
							},
						},
					},
				},
			},
		},
		{
			name:     "successful apply",
			expected: "some err\nsome err",
			status: v1alpha1.ManifestWorkStatus{
				Name: "manifest",
				Status: mwv1.ManifestResourceStatus{
					Manifests: []mwv1.ManifestCondition{
						{
							Conditions: []v1.Condition{
								{
									Type:   "Applied",
									Status: "True",
								},
							},
							StatusFeedbacks: mwv1.StatusFeedbackResult{
								Values: []mwv1.FeedbackValue{
									{
										Name: "ConditionReason",
										Value: mwv1.FieldValue{
											String: &e,
										},
									},
									{
										Name: "ConditionMessage",
										Value: mwv1.FieldValue{
											String: &e,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := GetConditionMessageFromManifestWorkStatus(&test.status)
			assert.Equal(t, test.expected, got)
		})
	}
}

func TestGenerateCGUForPlanItem(t *testing.T) {
	ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "ibu",
			Namespace: "namespace",
		},
		Spec: ibguv1alpha1.ImageBasedGroupUpgradeSpec{
			Plan: []ibguv1alpha1.PlanItem{
				{
					Actions: []string{
						ibguv1alpha1.Prep,
						ibguv1alpha1.Upgrade,
						ibguv1alpha1.FinalizeUpgrade,
					},
					RolloutStrategy: ibguv1alpha1.RolloutStrategy{
						Timeout:        50,
						MaxConcurrency: 2,
					},
				},
			},
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

	cgu := GenerateClusterGroupUpgradeForPlanItem("ibu-prep-upgrade-finalize", ibgu, &ibgu.Spec.Plan[0], []string{"ibu-prep", "ibu-upgrade", "ibu-finalize"}, []string{"cluster1", "cluster2"}, map[string]string{}, true)

	json, _ := ObjectToJSON(cgu)
	expected := `
    {
  "apiVersion": "ran.openshift.io/v1alpha1",
  "kind": "ClusterGroupUpgrade",
  "metadata": {
    
    "labels": {
      "ibgu": "ibu"
    },
    "name": "ibu-prep-upgrade-finalize",
    "namespace": "namespace"
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
      "ibu-prep",
      "ibu-upgrade",
      "ibu-finalize"
    ],
    "preCachingConfigRef": {},
    "remediationStrategy": {
      "maxConcurrency": 2,
      "timeout": 50
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
    `
	assert.JSONEq(t, expected, json)
}

func TestUpgradeManifestworkReplicaset(t *testing.T) {
	mwrs, _ := GenerateUpgradeManifestWorkReplicaset("ibu-upgrade", "namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":1,\"name\":\"isUpgradeCompleted\",\"value\":\"True\"}]"
    },
    
    "name": "ibu-upgrade",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": "Orphan"
      },
      "manifestConfigs": [
        {
          "feedbackRules": [
            {
              "jsonPaths": [
                {
                  "name": "isUpgradeCompleted",
                  "path": ".status.conditions[?(@.type==\"UpgradeCompleted\")].status"
                },
                {
                  "name": "upgradeInProgressConditionMessage",
                  "path": ".status.conditions[?(@.type==\"UpgradeInProgress\")].message"
                },
                {
                  "name": "upgradeCompletedConditionMessages",
                  "path": ".status.conditions[?(@.type==\"UpgradeCompleted\")].message"
                }
              ],
              "type": "JSONPaths"
            }
          ],
          "resourceIdentifier": {
            "group": "lca.openshift.io",
            "name": "upgrade",
            "namespace": "",
            "resource": "imagebasedupgrades"
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {
              
              "labels": {
                "open-cluster-management.io/aggregate-to-work": "true"
              },
              "name": "open-cluster-management:klusterlet-work:ibu-role"
            },
            "rules": [
              {
                "apiGroups": [
                  "lca.openshift.io"
                ],
                "resources": [
                  "imagebasedupgrades"
                ],
                "verbs": [
                  "get",
                  "list",
                  "watch",
                  "create",
                  "update",
                  "patch",
                  "delete"
                ]
              }
            ]
          },
          {
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              
              "name": "upgrade"
            },
            "spec": {
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Upgrade"
            },
            "status": {
              "rollbackAvailabilityExpiration": null
            }
          }
        ]
      }
    },
    "placementRefs": [
      {
        "name": "dummy",
        "rolloutStrategy": {}
      }
    ]
  },
  "status": {
    "placementSummary": null,
    "summary": {
      "applied": 0,
      "available": 0,
      "degraded": 0,
      "desiredTotal": 0,
      "progressing": 0,
      "total": 0,
      "updated": 0
    }
  }
}
    `
	json, err := ObjectToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}

func TestAbortManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := GenerateAbortManifestWorkReplicaset("ibu-abort", "namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":1,\"name\":\"isIdle\",\"value\":\"True\"}]"
    },
    
    "name": "ibu-abort",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": "Orphan"
      },
      "manifestConfigs": [
        {
          "feedbackRules": [
            {
              "jsonPaths": [
                {
                  "name": "isIdle",
                  "path": ".status.conditions[?(@.type==\"Idle\")].status"
                },
                {
                  "name": "idleConditionReason",
                  "path": ".status.conditions[?(@.type==\"Idle\")].reason"
                },
                {
                  "name": "idleConditionMessages",
                  "path": ".status.conditions[?(@.type==\"Idle\")].message"
                }
              ],
              "type": "JSONPaths"
            }
          ],
          "resourceIdentifier": {
            "group": "lca.openshift.io",
            "name": "upgrade",
            "namespace": "",
            "resource": "imagebasedupgrades"
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {
              
              "labels": {
                "open-cluster-management.io/aggregate-to-work": "true"
              },
              "name": "open-cluster-management:klusterlet-work:ibu-role"
            },
            "rules": [
              {
                "apiGroups": [
                  "lca.openshift.io"
                ],
                "resources": [
                  "imagebasedupgrades"
                ],
                "verbs": [
                  "get",
                  "list",
                  "watch",
                  "create",
                  "update",
                  "patch",
                  "delete"
                ]
              }
            ]
          },
          {
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              
              "name": "upgrade"
            },
            "spec": {
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Idle"
            },
            "status": {
              "rollbackAvailabilityExpiration": null
            }
          }
        ]
      }
    },
    "placementRefs": [
      {
        "name": "dummy",
        "rolloutStrategy": {}
      }
    ]
  },
  "status": {
    "placementSummary": null,
    "summary": {
      "applied": 0,
      "available": 0,
      "degraded": 0,
      "desiredTotal": 0,
      "progressing": 0,
      "total": 0,
      "updated": 0
    }
  }
}
    `
	json, err := ObjectToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}
func TestFinalizeManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := GenerateFinalizeManifestWorkReplicaset("ibu-finalize", "namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":1,\"name\":\"isIdle\",\"value\":\"True\"}]"
    },
    
    "name": "ibu-finalize",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": "Orphan"
      },
      "manifestConfigs": [
        {
          "feedbackRules": [
            {
              "jsonPaths": [
                {
                  "name": "isIdle",
                  "path": ".status.conditions[?(@.type==\"Idle\")].status"
                },
                {
                  "name": "idleConditionReason",
                  "path": ".status.conditions[?(@.type==\"Idle\")].reason"
                },
                {
                  "name": "idleConditionMessages",
                  "path": ".status.conditions[?(@.type==\"Idle\")].message"
                }
              ],
              "type": "JSONPaths"
            }
          ],
          "resourceIdentifier": {
            "group": "lca.openshift.io",
            "name": "upgrade",
            "namespace": "",
            "resource": "imagebasedupgrades"
          }
        }
      ],
      "workload": {
        "manifests": [
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {
              
              "labels": {
                "open-cluster-management.io/aggregate-to-work": "true"
              },
              "name": "open-cluster-management:klusterlet-work:ibu-role"
            },
            "rules": [
              {
                "apiGroups": [
                  "lca.openshift.io"
                ],
                "resources": [
                  "imagebasedupgrades"
                ],
                "verbs": [
                  "get",
                  "list",
                  "watch",
                  "create",
                  "update",
                  "patch",
                  "delete"
                ]
              }
            ]
          },
          {
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              
              "name": "upgrade"
            },
            "spec": {
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Idle"
            },
            "status": {
              "rollbackAvailabilityExpiration": null
            }
          }
        ]
      }
    },
    "placementRefs": [
      {
        "name": "dummy",
        "rolloutStrategy": {}
      }
    ]
  },
  "status": {
    "placementSummary": null,
    "summary": {
      "applied": 0,
      "available": 0,
      "degraded": 0,
      "desiredTotal": 0,
      "progressing": 0,
      "total": 0,
      "updated": 0
    }
  }
}
    `
	json, err := ObjectToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}

func TestRollbackManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := GenerateRollbackManifestWorkReplicaset("ibu-prep", "namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":1,\"name\":\"isRollbackCompleted\",\"value\":\"True\"}]"
    },
    
    "name": "ibu-prep",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": "Orphan"
      },
      "manifestConfigs": [
        {
          "feedbackRules": [
            {
              "jsonPaths": [
                {
                  "name": "isRollbackCompleted",
                  "path": ".status.conditions[?(@.type==\"RollbackCompleted\")].status"
                },
                {
                  "name": "rollbackInProgressConditionMessage",
                  "path": ".status.conditions[?(@.type==\"RollbackInProgress\")].message"
                },
                {
                  "name": "rollbackCompletedConditionMessages",
                  "path": ".status.conditions[?(@.type==\"RollbackCompleted\")].message"
                }
              ],
              "type": "JSONPaths"
            }
          ],
          "resourceIdentifier": {
            "group": "lca.openshift.io",
            "name": "upgrade",
            "namespace": "",
            "resource": "imagebasedupgrades"
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {
              
              "labels": {
                "open-cluster-management.io/aggregate-to-work": "true"
              },
              "name": "open-cluster-management:klusterlet-work:ibu-role"
            },
            "rules": [
              {
                "apiGroups": [
                  "lca.openshift.io"
                ],
                "resources": [
                  "imagebasedupgrades"
                ],
                "verbs": [
                  "get",
                  "list",
                  "watch",
                  "create",
                  "update",
                  "patch",
                  "delete"
                ]
              }
            ]
          },
          {
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              
              "name": "upgrade"
            },
            "spec": {
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Rollback"
            },
            "status": {
              "rollbackAvailabilityExpiration": null
            }
          }
        ]
      }
    },
    "placementRefs": [
      {
        "name": "dummy",
        "rolloutStrategy": {}
      }
    ]
  },
  "status": {
    "placementSummary": null,
    "summary": {
      "applied": 0,
      "available": 0,
      "degraded": 0,
      "desiredTotal": 0,
      "progressing": 0,
      "total": 0,
      "updated": 0
    }
  }
}
    `
	json, err := ObjectToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}
func TestPrepManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := GeneratePrepManifestWorkReplicaset("ibu-prep", "namespace", ibu, []mwv1.Manifest{})
	expectedRaw := `
{
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":1,\"name\":\"isPrepCompleted\",\"value\":\"True\"}]"
    },
    
    "name": "ibu-prep",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": "Orphan"
      },
      "manifestConfigs": [
        {
          "feedbackRules": [
            {
              "jsonPaths": [
                {
                  "name": "isPrepCompleted",
                  "path": ".status.conditions[?(@.type==\"PrepCompleted\")].status"
                },
                {
                  "name": "prepInProgressConditionMessage",
                  "path": ".status.conditions[?(@.type==\"PrepInProgress\")].message"
                },
                {
                  "name": "prepCompletedConditionMessages",
                  "path": ".status.conditions[?(@.type==\"PrepCompleted\")].message"
                }
              ],
              "type": "JSONPaths"
            }
          ],
          "resourceIdentifier": {
            "group": "lca.openshift.io",
            "name": "upgrade",
            "namespace": "",
            "resource": "imagebasedupgrades"
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {
              
              "labels": {
                "open-cluster-management.io/aggregate-to-work": "true"
              },
              "name": "open-cluster-management:klusterlet-work:ibu-role"
            },
            "rules": [
              {
                "apiGroups": [
                  "lca.openshift.io"
                ],
                "resources": [
                  "imagebasedupgrades"
                ],
                "verbs": [
                  "get",
                  "list",
                  "watch",
                  "create",
                  "update",
                  "patch",
                  "delete"
                ]
              }
            ]
          },
          {
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              
              "name": "upgrade"
            },
            "spec": {
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Prep"
            },
            "status": {
              "rollbackAvailabilityExpiration": null
            }
          }
        ]
      }
    },
    "placementRefs": [
      {
        "name": "dummy",
        "rolloutStrategy": {}
      }
    ]
  },
  "status": {
    "placementSummary": null,
    "summary": {
      "applied": 0,
      "available": 0,
      "degraded": 0,
      "desiredTotal": 0,
      "progressing": 0,
      "total": 0,
      "updated": 0
    }
  }
}`
	json, err := ObjectToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}

func TestGetActionsFromCGU(t *testing.T) {
	tests := []struct {
		name      string
		templates []string
		expected  []ibguv1alpha1.ActionMessage
	}{
		{
			name: "hi",
			templates: []string{
				"name-prep", "name-upgrade", "name-finalizeupgrade", "name-abort", "name-gd-rollback",
			},
			expected: []ibguv1alpha1.ActionMessage{
				{Action: ibguv1alpha1.Prep},
				{Action: ibguv1alpha1.Upgrade}, {Action: ibguv1alpha1.FinalizeUpgrade},
				{Action: ibguv1alpha1.Abort}, {Action: ibguv1alpha1.Rollback},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cgu := &v1alpha1.ClusterGroupUpgrade{
				Spec: v1alpha1.ClusterGroupUpgradeSpec{
					ManifestWorkTemplates: test.templates,
				},
			}
			got := GetAllActionMessagesFromCGU(cgu)
			assert.Equal(t, test.expected, got)
		})
	}
}

func TestClusterMatchesPlanItem(t *testing.T) {
	const (
		prepCompleted     = "lcm.openshift.io/ibgu-prep-completed"
		upgradeCompleted  = "lcm.openshift.io/ibgu-upgrade-completed"
		rollbackCompleted = "lcm.openshift.io/ibgu-rollback-completed"
		prepFailed        = "lcm.openshift.io/ibgu-prep-failed"
		upgradeFailed     = "lcm.openshift.io/ibgu-upgrade-failed"
	)
	tests := []struct {
		name          string
		clusterLabels map[string]string
		planItem      ibguv1alpha1.PlanItem
		expected      bool
	}{
		{
			name:          "Prep matches any cluster (base set)",
			clusterLabels: map[string]string{"common": "true"},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Prep}},
			expected:      true,
		},
		{
			name:          "composite Prep-first item matches base set",
			clusterLabels: nil,
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Prep, ibguv1alpha1.Upgrade}},
			expected:      true,
		},
		{
			name:          "Upgrade matches only prep-completed clusters",
			clusterLabels: map[string]string{prepCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Upgrade}},
			expected:      true,
		},
		{
			name:          "Upgrade skips clusters without prep-completed",
			clusterLabels: map[string]string{"common": "true"},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Upgrade}},
			expected:      false,
		},
		{
			name:          "Abort matches clusters without upgrade-completed",
			clusterLabels: map[string]string{prepCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Abort}},
			expected:      true,
		},
		{
			name:          "Abort skips clusters with upgrade-completed",
			clusterLabels: map[string]string{upgradeCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Abort}},
			expected:      false,
		},
		{
			name:          "Rollback matches upgrade-completed clusters",
			clusterLabels: map[string]string{upgradeCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Rollback}},
			expected:      true,
		},
		{
			name:          "Rollback skips clusters without upgrade-completed",
			clusterLabels: map[string]string{prepCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Rollback}},
			expected:      false,
		},
		{
			name:          "FinalizeUpgrade matches upgrade-completed clusters",
			clusterLabels: map[string]string{upgradeCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.FinalizeUpgrade}},
			expected:      true,
		},
		{
			name:          "FinalizeUpgrade skips clusters without upgrade-completed",
			clusterLabels: map[string]string{prepCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.FinalizeUpgrade}},
			expected:      false,
		},
		{
			name:          "FinalizeRollback matches rollback-completed clusters",
			clusterLabels: map[string]string{rollbackCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.FinalizeRollback}},
			expected:      true,
		},
		{
			name:          "FinalizeRollback skips clusters without rollback-completed",
			clusterLabels: map[string]string{upgradeCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.FinalizeRollback}},
			expected:      false,
		},
		{
			name:          "AbortOnFailure matches prep-failed clusters",
			clusterLabels: map[string]string{prepFailed: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.AbortOnFailure}},
			expected:      true,
		},
		{
			name:          "AbortOnFailure matches upgrade-failed clusters",
			clusterLabels: map[string]string{upgradeFailed: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.AbortOnFailure}},
			expected:      true,
		},
		{
			name:          "AbortOnFailure skips clusters without a failed label",
			clusterLabels: map[string]string{prepCompleted: ""},
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.AbortOnFailure}},
			expected:      false,
		},
		{
			name:          "cluster with no labels only matches base-set items",
			clusterLabels: nil,
			planItem:      ibguv1alpha1.PlanItem{Actions: []string{ibguv1alpha1.Upgrade}},
			expected:      false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClusterMatchesPlanItem(test.clusterLabels, &test.planItem)
			assert.Equal(t, test.expected, got)
		})
	}
}
