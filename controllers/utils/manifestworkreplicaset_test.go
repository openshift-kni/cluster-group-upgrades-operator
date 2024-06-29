package utils

import (
	"testing"

	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
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
	ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:      "ibu",
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
			Actions: []ibguv1alpha1.ImageBasedUpgradeAction{
				ibguv1alpha1.Prep,
				ibguv1alpha1.Upgrade,
				ibguv1alpha1.Finalize,
			},
			IBUSpec: lcav1.ImageBasedUpgradeSpec{
				SeedImageRef: lcav1.SeedImageRef{
					Version: "version",
					Image:   "image",
				},
			},
		},
	}
	templateNames := []string{"ibu-prep", "ibu-upgrade", "ibu-finalize"}
	cgu := GenerateClusterGroupUpgradeForIBGU(ibgu, templateNames, []string{"name-action"})

	json, _ := objToJSON(cgu)
	expected := `
    {
  "apiVersion": "ran.openshift.io/v1alpha1",
  "kind": "ClusterGroupUpgrade",
  "metadata": {
    "creationTimestamp": null,
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
    "blockingCRs": [
      {
        "name": "name-action",
        "namespace": "namespace"
      }
    ],
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
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isUpgradeCompleted\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
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
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
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
              "completedAt": null,
              "rollbackAvailabilityExpiration": null,
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
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isIdle\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
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
            "resource": "imagebasedupgrades"
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
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
              "completedAt": null,
              "rollbackAvailabilityExpiration": null,
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
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isIdle\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
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
            "resource": "imagebasedupgrades"
          }
        }
      ],
      "workload": {
        "manifests": [
          {
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
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
              "completedAt": null,
              "rollbackAvailabilityExpiration": null,
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
      "openshift-cluster-group-upgrades/expectedValues": "[{\"manifestIndex\":0,\"name\":\"isRollbackCompleted\",\"value\":\"True\"}]"
    },
    "creationTimestamp": null,
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
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
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
              "completedAt": null,
              "rollbackAvailabilityExpiration": null,
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
	mwrs, _ := GeneratePrepManifestWorkReplicaset("ibu-prep", "namespace", ibu)
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
            "apiVersion": "lca.openshift.io/v1",
            "kind": "ImageBasedUpgrade",
            "metadata": {
              "creationTimestamp": null,
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
              "completedAt": null,
              "rollbackAvailabilityExpiration": null,
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
