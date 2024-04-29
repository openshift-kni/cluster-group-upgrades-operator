package utils

import (
	"testing"

	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	lcav1alpha1 "github.com/openshift-kni/lifecycle-agent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

var ibu = &lcav1alpha1.ImageBasedUpgrade{
	ObjectMeta: v1.ObjectMeta{
		Name: "upgrade",
	},
	Spec: lcav1alpha1.ImageBasedUpgradeSpec{
		SeedImageRef: lcav1alpha1.SeedImageRef{
			Image:   "quay.io/image/version:tag",
			Version: "14.4.0-rc.2",
		},
	},
}

func objToJSON(obj runtime.Object) (string, error) {
	scheme := runtime.NewScheme()
	mwv1alpha1.AddToScheme(scheme)
	v1alpha1.AddToScheme(scheme)
	outUnstructured := &unstructured.Unstructured{}
	scheme.Convert(obj, outUnstructured, nil)
	json, err := outUnstructured.MarshalJSON()
	return string(json), err
}

func TestGetCGUForCGUIBU(t *testing.T) {
	cgibu := &v1alpha1.ClusterGroupImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: v1alpha1.ClusterGroupImageBasedUpgradeSpec{

			RolloutStrategy: v1alpha1.RolloutStrategy{
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
			Actions: []v1alpha1.ImageBasedUpgradeAction{
				"Prep",
				"Upgrade",
				"Finalize",
			},
			IBUSpec: lcav1alpha1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1alpha1.SeedImageRef{
					Version: "version",
					Image:   "image",
				},
			},
		},
	}
	cgu := getClusterGroupUpgradeForCGIBU(cgibu)

	json, _ := objToJSON(cgu)
	expected := `
    {
  "apiVersion": "ran.openshift.io/v1alpha1",
  "kind": "ClusterGroupUpgrade",
  "metadata": {
    "creationTimestamp": null,
    "name": "ibu-upgrade",
    "namespace": "namespace"
  },
  "spec": {
    "actions": {},
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
	mwrs, _ := upgradeManifestWorkReplicaset("namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isUpgradeCompleted\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
    "name": "ibu-upgrade",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": ""
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
            "resource": ""
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "lca.openshift.io/v1alpha1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
              "name": "upgrade"
            },
            "spec": {
              "autoRollbackOnFailure": {},
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Upgrade"
            },
            "status": {
              "completedAt": null,
              "startedAt": null
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
	json, err := objToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}

func TestAbortManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1alpha1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1alpha1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1alpha1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := abortManifestWorkReplicaset("namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isIdle\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
    "name": "ibu-abort",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": ""
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
                  "name": "idleConditionMessage",
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
            "resource": ""
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "lca.openshift.io/v1alpha1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
              "name": "upgrade"
            },
            "spec": {
              "autoRollbackOnFailure": {},
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Idle"
            },
            "status": {
              "completedAt": null,
              "startedAt": null
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
	json, err := objToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}
func TestFinalizeManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1alpha1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1alpha1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1alpha1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := finalizeManifestWorkReplicaset("namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isIdle\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
    "name": "ibu-finalize",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": ""
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
                  "name": "idleConditionMessage",
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
            "resource": ""
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "lca.openshift.io/v1alpha1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
              "name": "upgrade"
            },
            "spec": {
              "autoRollbackOnFailure": {},
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Idle"
            },
            "status": {
              "completedAt": null,
              "startedAt": null
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
	json, err := objToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}

func TestRollbackManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1alpha1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1alpha1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1alpha1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := rollbackManifestWorkReplicaset("namespace", ibu)
	expectedRaw := `
    {
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isRollbackCompleted\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
    "name": "ibu-prep",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": ""
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
            "resource": ""
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "lca.openshift.io/v1alpha1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
              "name": "upgrade"
            },
            "spec": {
              "autoRollbackOnFailure": {},
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Rollback"
            },
            "status": {
              "completedAt": null,
              "startedAt": null
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
	json, err := objToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}
func TestPrepManifestworkReplicaset(t *testing.T) {
	ibu := &lcav1alpha1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: lcav1alpha1.ImageBasedUpgradeSpec{
			SeedImageRef: lcav1alpha1.SeedImageRef{
				Image:   "quay.io/image/version:tag",
				Version: "14.4.0-rc.2",
			},
		},
	}
	mwrs, _ := prepManifestWorkReplicaset("namespace", ibu)
	expectedRaw := `
{
  "apiVersion": "work.open-cluster-management.io/v1alpha1",
  "kind": "ManifestWorkReplicaSet",
  "metadata": {
    "annotations": {
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isPrepCompleted\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
    "name": "ibu-prep",
    "namespace": "namespace"
  },
  "spec": {
    "manifestWorkTemplate": {
      "deleteOption": {
        "propagationPolicy": ""
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
            "resource": ""
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "lca.openshift.io/v1alpha1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
              "name": "upgrade"
            },
            "spec": {
              "autoRollbackOnFailure": {},
              "seedImageRef": {
                "image": "quay.io/image/version:tag",
                "version": "14.4.0-rc.2"
              },
              "stage": "Prep"
            },
            "status": {
              "completedAt": null,
              "startedAt": null
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
	json, err := objToJSON(mwrs)
	if err != nil {
		panic(err)
	}
	assert.JSONEq(t, expectedRaw, json)
}
