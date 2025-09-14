package utils

import (
	"testing"

	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			name:     "successfull apply",
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
			name:     "successfull apply",
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

	cgu := GenerateClusterGroupUpgradeForPlanItem("ibu-prep-upgrade-finalize", ibgu, &ibgu.Spec.Plan[0], []string{"ibu-prep", "ibu-upgrade", "ibu-finalize"}, map[string]string{}, true)

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
    "clusterLabelSelectors": [
      {
        "matchLabels": {
          "common": "true"
        }
      }
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
                  "path": ".status.conditions[?(@.type==\"UpgradeInProgress\")].message'"
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
      "Applied": 0,
      "available": 0,
      "degraded": 0,
      "progressing": 0,
      "total": 0
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
                  "path": ".status.conditions[?(@.type==\"Idle\")].reason'"
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
      "Applied": 0,
      "available": 0,
      "degraded": 0,
      "progressing": 0,
      "total": 0
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
                  "path": ".status.conditions[?(@.type==\"Idle\")].reason'"
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
      "Applied": 0,
      "available": 0,
      "degraded": 0,
      "progressing": 0,
      "total": 0
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
                  "path": ".status.conditions[?(@.type==\"RollbackInProgress\")].message'"
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
      "Applied": 0,
      "available": 0,
      "degraded": 0,
      "progressing": 0,
      "total": 0
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
                  "path": ".status.conditions[?(@.type==\"PrepInProgress\")].message'"
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
      "Applied": 0,
      "available": 0,
      "degraded": 0,
      "progressing": 0,
      "total": 0
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

func TestGetLabelSelectorForPlanItem(t *testing.T) {
	tests := []struct {
		name           string
		currentSelctor []metav1.LabelSelector
		expected       []metav1.LabelSelector
		planItem       ibguv1alpha1.PlanItem
	}{
		{
			name: "no extra selector",
			currentSelctor: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
				},
			},
			expected: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
				},
			},
			planItem: ibguv1alpha1.PlanItem{
				Actions: []string{ibguv1alpha1.Prep},
			},
		},
		{
			name: "finalizeUpgrade",
			currentSelctor: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
				},
			},
			expected: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true",
						"lcm.openshift.io/ibgu-upgrade-completed": ""},
				},
			},
			planItem: ibguv1alpha1.PlanItem{
				Actions: []string{ibguv1alpha1.FinalizeUpgrade},
			},
		},
		{
			name: "abortOnFailure",
			currentSelctor: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
				},
			},
			expected: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true",
						"lcm.openshift.io/ibgu-upgrade-failed": ""},
				},
				{
					MatchLabels: map[string]string{"common": "true",
						"lcm.openshift.io/ibgu-prep-failed": ""},
				},
			},
			planItem: ibguv1alpha1.PlanItem{
				Actions: []string{ibguv1alpha1.AbortOnFailure},
			},
		},
		{
			name: "finalizeRollback",
			currentSelctor: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
				},
			},
			expected: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true",
						"lcm.openshift.io/ibgu-rollback-completed": ""},
				},
			},
			planItem: ibguv1alpha1.PlanItem{
				Actions: []string{ibguv1alpha1.FinalizeRollback},
			},
		},
		{
			name: "Rollback",
			currentSelctor: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
				},
				{
					MatchLabels: map[string]string{"group": "true"},
				},
			},
			expected: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true",
						"lcm.openshift.io/ibgu-upgrade-completed": ""},
				},
				{
					MatchLabels: map[string]string{"group": "true",
						"lcm.openshift.io/ibgu-upgrade-completed": ""},
				},
			},
			planItem: ibguv1alpha1.PlanItem{
				Actions: []string{ibguv1alpha1.Rollback},
			},
		},
		{
			name: "Abort",
			currentSelctor: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
				},
			},
			expected: []v1.LabelSelector{
				{
					MatchLabels: map[string]string{"common": "true"},
					MatchExpressions: []v1.LabelSelectorRequirement{
						{
							Key:      "lcm.openshift.io/ibgu-upgrade-completed",
							Operator: metav1.LabelSelectorOpDoesNotExist,
						},
					},
				},
			},
			planItem: ibguv1alpha1.PlanItem{
				Actions: []string{ibguv1alpha1.Abort},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := getLabelSelectorForPlanItem(test.currentSelctor, &test.planItem)
			assert.ElementsMatch(t, test.expected, got)
		})
	}
}
