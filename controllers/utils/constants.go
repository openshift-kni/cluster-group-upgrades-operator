package utils

// RemediationActionEnforce - Policy remediation for policies.
const (
	RemediationActionEnforce = "enforce"
	RemediationActionInform  = "inform"
)

// Possible status returned when checking the compliance of a cluster with a policy.
const (
	ClusterStatusNonCompliant   = "NonCompliant"
	ClusterStatusCompliant      = "Compliant"
	ClusterNotMatchedWithPolicy = "NotMatchedWithPolicy"
	ClusterStatusUnknown        = "ClusterStatusUnknown"
	PolicyStatusPresent         = "PolicyStatusPresent"
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
	MaxPolicyNameLength    = 63
	MaxObjectNameLength    = 253
	NameSuffixAnnotation   = CsvNamePrefix + "/name-suffix"
	RandomNameSuffixLength = 5
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
	PolicyTypeSubscription   = "Subscription"
	PolicyTypeClusterVersion = "ClusterVersion"
	PolicyTypeCatalogSource  = "CatalogSource"
)

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

// Policy errors
const (
	PlcMissTmplDef           = "policy is missing its spec.policy-templates.objectDefinition"
	PlcMissTmplDefMeta       = "policy is missing its spec.policy-templates.objectDefinition.metadata"
	PlcMissTmplDefSpec       = "policy is missing its spec.policy-templates.objectDefinition.spec"
	ConfigPlcMissObjTmpl     = "policy is missing its spec.policy-templates.objectDefinition.spec.object-templates"
	ConfigPlcMissObjTmplDef  = "policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.objectDefinition"
	PlcHasHubTmplErr         = "policy has hub template error, check the configuration policy's annotation 'policy.open-cluster-management.io/hub-templates-error' for detail"
	PlcHubTmplFmtErr         = "template format is not supported in TALM"
	PlcHubTmplFuncErr        = "template function is not supported in TALM"
	PlcHubTmplPrinfInNameErr = "printf variable is not supported in the template function Name field"
	PlcHubTmplPrinfInNsErr   = "printf variable is not supported in the template function Namespace field"
)
