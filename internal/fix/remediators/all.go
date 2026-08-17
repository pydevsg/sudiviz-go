package remediators

import "github.com/pydevsg/sudiviz-go/internal/fix"

// All returns the built-in remediator set in match-priority order.
func All() []fix.Remediator {
	return []fix.Remediator{
		SGIngress{},
		OrphanTargetGroup{},
		UnusedSecurityGroup{},
		S3PublicAccess{},
		S3Encryption{},
		RDSPublicAccess{},
		UnhealthyTargets{},
	}
}
