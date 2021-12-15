package utils

const (
	RemediationActionEnforce = "enforce"
	RemediationActionInform  = "inform"

	StatusNonCompliant          = "NonCompliant"
	StatusCompliant             = "Compliant"
	ClusterNotMatchedWithPolicy = "NotMatchedWithPolicy"
	StatusUnknown               = "StatusUnknown"

	NoPolicyIndex        = -1
	AllPoliciesValidated = -2

	ChildPolicyLabel = "policy.open-cluster-management.io/root-policy"
)
