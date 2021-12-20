package utils

// RemediationActionEnforce - Policy remediation for policies.
const (
	RemediationActionEnforce = "enforce"
	RemediationActionInform  = "inform"
)

// Possible status returned when checking the compliance of a cluster with a policy.
const (
	StatusNonCompliant          = "NonCompliant"
	StatusCompliant             = "Compliant"
	ClusterNotMatchedWithPolicy = "NotMatchedWithPolicy"
	StatusUnknown               = "StatusUnknown"
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

// Pre-cache
const (
	KubeconfigSecretSuffix     = "admin-kubeconfig"
	OperatorConfigOverrides    = "cluster-group-upgrade-overrides"
	PrecacheJobNamespace       = "pre-cache"
	PrecacheJobName            = "pre-cache"
	PrecacheServiceAccountName = "pre-cache-agent"
	PrecacheSpecCmName         = "pre-cache-spec"
	JobNotFoundString          = "jobs.batch \"pre-cache\" not found"

	PrecacheNotStarted         = "NotStarted"
	PrecacheStarting           = "Starting"
	PrecacheFailedToStart      = "FailedToStart"
	PrecacheActive             = "Active"
	PrecacheSucceeded          = "Succeeded"
	PrecachePartiallyDone      = "PartiallyDone"
	PrecacheUnrecoverableError = "UnrecoverableError"
	PrecacheUnforeseenStatus   = "UnforeseenStatus"
)
