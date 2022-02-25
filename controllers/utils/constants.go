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
	PolicyStatusUnknown         = "StatusUnknown"
	PolicyStatusPresent         = "PolicyStatusPresent"
)

// Indexes for managed policies in the CurrentRemediationPolicyIndex.
const (
	NoPolicyIndex        = -1
	AllPoliciesValidated = -2
)

// Label specific to ACM child policies.
const (
	ChildPolicyLabel = "policy.open-cluster-management.io/root-policy"
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

// ViewUpdateSec defines default ManagementClusterView update periodicity
// When configuring managedclusterview for clusters in precache-starting state,
// this value is multiplied by number of clusters
const ViewUpdateSec = 20

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
	MultiCloudWaitTimeSec           = 3

	TestManagedClusterActionTimeoutMessage = `ManagedClusterAction hasn't completed in the required timeout`
	TestManagedClusterActionFailedMessage  = "ManagedClusterAction failed"
)

// Upgrade specific constants.
const (
	MaxNumberOfClustersForUpgrade = 100
)
