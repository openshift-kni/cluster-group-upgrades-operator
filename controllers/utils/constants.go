package utils

import "k8s.io/apimachinery/pkg/runtime/schema"

// CGU controller constants
const (
	CGUControllerWorkerCountEnv     = "TALM_CGU_CTRL_WORKER_COUNT"
	DefaultCGUControllerWorkerCount = 5
)

// RemediationActionEnforce - Policy remediation for policies.
const (
	RemediationActionEnforce = "enforce"
	RemediationActionInform  = "inform"
)

// Possible status returned when checking the compliance of a cluster with a policy.
const (
	ClusterStatusNonCompliant   = "NonCompliant"
	ClusterStatusCompliant      = "Compliant"
	ClusterStatusPending        = "Pending"
	ClusterNotMatchedWithPolicy = "NotMatchedWithPolicy"
	PolicyStatusPresent         = "PolicyStatusPresent"
)

// Possible status of cluster remediation progress
const (
	ClusterRemediationComplete = "complete"
	ClusterRemediationTimedout = "timedout"
)

// Label specific to ACM child policies.
const (
	ChildPolicyLabel = "policy.open-cluster-management.io/root-policy"
)

// Annotation for TALO created object names
const (
	DesiredResourceName = CsvNamePrefix + "/rname"
)

// CR name length limits and suffix annotation
const (
	// Maximum length of policy + namespace (not including extra separator dot so 63 -1 = 62)
	// this is calculated with utf8.RuneCountInString(policy.Name)+utf8.RuneCountInString+utf8.RuneCountInString(policy.Namespace)
	MaxPolicyNameLengthExcludingTheDot = 62
	MaxObjectNameLength                = 253
	NameSuffixAnnotation               = CsvNamePrefix + "/name-suffix"
	RandomNameSuffixLength             = 5
)

// Pre-cache constants
const (
	CsvNamePrefix              = "cluster-group-upgrades-operator"
	KubeconfigSecretSuffix     = "admin-kubeconfig"
	OperatorConfigOverrides    = "cluster-group-upgrade-overrides"
	PrecacheJobNamespace       = "openshift-talo-pre-cache"
	PrecacheJobName            = "pre-cache"
	PrecacheServiceAccountName = "pre-cache-agent"
	PrecacheSpecCmName         = "pre-cache-spec"
	PrecacheSpecValidCondition = "PrecacheSpecValid"
)

// ViewUpdateSecPerCluster defines default ManagementClusterView update periodicity
// When configuring managedclusterview for clusters in precache-starting state,
// this value is multiplied by number of clusters, then bound by min and max
const (
	ViewUpdateSecPerCluster = 6
	ViewUpdateSecTotalMin   = 30
	ViewUpdateSecTotalMax   = 300
)

// Policy types used within the operator
const (
	PolicyTypeCatalogSource = "CatalogSource"
)

// SubscriptionGroupVersionKind for monitoring and other type specific logic
func SubscriptionGroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Kind: "Subscription", Group: "operators.coreos.com"}
}

// ClusterVersionGroupVersionKind for monitoring and other type specific logic
func ClusterVersionGroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Kind: "ClusterVersion", Group: "config.openshift.io"}
}

// Subscription possible states
const (
	SubscriptionStateAtLatestKnown  = "AtLatestKnown"
	SubscriptionStateUpgradePending = "UpgradePending"
)

// Multicloud object types
const (
	ManagedClusterViewPrefix   = "view"
	ManagedClusterActionPrefix = "action"
)

// Constants used for working with multicloud-operators-foundation
const (
	InstallPlanWasApproved          = 0
	InstallPlanCannotBeApproved     = 1
	NoActionForApprovingInstallPlan = 2
	MultiCloudPendingStatus         = 3
	InstallPlanAlreadyApproved      = 4

	MultiCloudWaitTimeSec = 3

	TestManagedClusterActionTimeoutMessage = `ManagedClusterAction hasn't completed in the required timeout`
	TestManagedClusterActionFailedMessage  = "ManagedClusterAction failed"
)

// Reconciling instructions.
const (
	ReconcileNow    = 0
	StopReconciling = 1
	DontReconcile   = 2
)

// Finalizers
const (
	CleanupFinalizer = "ran.openshift.io/cleanup-finalizer"
)

// Upgrade status
const (
	CannotStart = "UpgradeCannotStart"
)

// ExcludeFromClusterBackup is a label to exclude object from cluster-backup-operator
// https://github.com/stolostron/cluster-backup-operator#steps-to-identify-backup-data
const ExcludeFromClusterBackup = "velero.io/exclude-from-backup"

// Object template constants
const (
	ObjectTemplates    = "object-templates"
	ObjectTemplatesRaw = "object-templates-raw"
)

// Policy errors
const (
	PlcMissTmplDef           = "policy is missing its spec.policy-templates.objectDefinition"
	PlcMissTmplDefMeta       = "policy is missing its spec.policy-templates.objectDefinition.metadata"
	PlcMissTmplDefSpec       = "policy is missing its spec.policy-templates.objectDefinition.spec"
	ConfigPlcFailRawMarshal  = "policy was unable to be unmmarshalled from object-templates-raw"
	ConfigPlcHasBothObjTmpl  = "policy has both spec.policy-templates.objectDefinition.spec.object-templates and spec.policy-templates.objectDefinition.spec.object-templates-raw"
	ConfigPlcMissAnyObjTmpl  = "policy is missing both spec.policy-templates.objectDefinition.spec.object-templates and spec.policy-templates.objectDefinition.spec.object-templates-raw"
	ConfigPlcMissObjTmplDef  = "policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.objectDefinition"
	ConfigPlcRawObjTmplErr   = "policy defines spec.policy-templates.objectDefinition.spec.object-templates-raw but is empty"
	Placeholder              = "placeholder"
	PlcHasHubTmplErr         = "policy has hub template error, check the configuration policy's annotation 'policy.open-cluster-management.io/hub-templates-error' for detail"
	PlcHubTmplFmtErr         = "template format is not supported in TALM"
	PlcHubTmplFuncErr        = "template function is not supported in TALM"
	PlcHubTmplPrinfInNameErr = "printf variable is not supported in the template function Name field"
	PlcHubTmplPrinfInNsErr   = "printf variable is not supported in the template function Namespace field"
	PlcLookupFuncResErr      = "this template function only allows the resource with apiVersion in 'cluster.open-cluster-management.io', kind 'ManagedCluster' and empty namespace"
)

// SpaceRequiredForPrecache is an env variable for precaching job that indicates the amount of space required
// for precaching job. This is a rough estimate. 30 GiB for OCP images and 5 GiB as a buffer for operator images
// and other disk allocations that can happen between the job and the actual upgrade kicked off by the CGU.
const SpaceRequiredForPrecache = "35 GiB"

// SoakAnnotation is the annotation that can be set on policies, which indicates the least number of seconds
// which policies should be compliant before the cgu moves on from that policy
const SoakAnnotation = "ran.openshift.io/soak-seconds"

// BlockingCGUCompletionModeAnn is an annotation that can be set on CGU, in order to continue with dependent CGU even if
// the original CR is only partially completed
const (
	BlockingCGUCompletionModeAnn = "ran.openshift.io/blocking-cgu-completion-mode"
	BlockingCGUClusterFiltering  = "ran.openshift.io/blocking-cgu-cluster-filtering"
	PartialBlockingCGUCompletion = "partial"
)

// CGUOwnerIBGULabel is a label that is set on CGUs that are created for IBGUs. It denotes which IBGU is the
// owner of this CGU
const CGUOwnerIBGULabel = "ibgu"

const (
	IBGUActionCompletedLabelTemplate = "lcm.openshift.io/ibgu-%s-completed"
	IBGUActionFailedLabelTemplate    = "lcm.openshift.io/ibgu-%s-failed"
)
