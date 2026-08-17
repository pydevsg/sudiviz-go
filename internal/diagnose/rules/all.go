package rules

import "github.com/pydevsg/sudiviz-go/internal/diagnose"

// All returns the complete built-in rule set. Adding a new diagnostic check
// means implementing diagnose.Rule and appending it here.
func All() []diagnose.Rule {
	return []diagnose.Rule{
		UnhealthyTargets{},
		MissingSGIngress{},
		S3Public{},
		RDSPublic{},
		UnencryptedStorage{},
		OrphanTargetGroup{},
		OrphanInstance{},
		UnusedSecurityGroup{},
		UnhealthyECSServices{},
		UnhealthyEKSClusters{},
		UnhealthyRDSInstances{},
		UnhealthyLambdas{},
	}
}

// NewEngine builds a diagnostic engine with the full built-in rule set.
func NewEngine() *diagnose.Engine { return diagnose.NewEngine(All()...) }
