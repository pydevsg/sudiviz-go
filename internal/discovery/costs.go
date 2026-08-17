package discovery

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// FinOps cost estimation. Approximate on-demand us-east-1 pricing is used as
// a baseline — good enough for the cost heatmap, not for billing.

const hoursPerMonth = 730

var ec2HourlyPrices = map[string]float64{
	"t2.micro": 0.0116, "t2.small": 0.023, "t2.medium": 0.0464, "t2.large": 0.0928, "t2.xlarge": 0.1856,
	"t3.micro": 0.0104, "t3.small": 0.0208, "t3.medium": 0.0416, "t3.large": 0.0832, "t3.xlarge": 0.1664,
	"m5.large": 0.096, "m5.xlarge": 0.192, "m5.2xlarge": 0.384,
	"m6i.large": 0.096, "m6i.xlarge": 0.192,
	"c5.large": 0.085, "c5.xlarge": 0.17, "c5.2xlarge": 0.34,
	"r5.large": 0.126, "r5.xlarge": 0.252,
}

var rdsHourlyPrices = map[string]float64{
	"db.t3.micro": 0.017, "db.t3.small": 0.034, "db.t3.medium": 0.068, "db.t3.large": 0.136,
	"db.m5.large": 0.171, "db.m5.xlarge": 0.342, "db.m5.2xlarge": 0.684,
	"db.r5.large": 0.24, "db.r5.xlarge": 0.48,
}

const (
	albMonthlyBase        = 22.0
	nlbMonthlyBase        = 18.0
	eksHourly             = 0.10
	lambdaMonthlyEstimate = 5.0
	s3MonthlyEstimate     = 2.0
)

// EstimateInstanceCost returns the estimated monthly cost of an EC2 instance.
func EstimateInstanceCost(instanceType, state string) float64 {
	if state != "running" {
		return 0
	}
	hourly, ok := ec2HourlyPrices[instanceType]
	if !ok {
		hourly = 0.05
	}
	return hourly * hoursPerMonth
}

// EstimateRDSCost returns the estimated monthly cost of an RDS instance.
func EstimateRDSCost(instanceClass, status string, multiAZ bool) float64 {
	if status != "available" && status != "backing-up" && status != "modifying" {
		return 0
	}
	hourly, ok := rdsHourlyPrices[instanceClass]
	if !ok {
		hourly = 0.10
	}
	base := hourly * hoursPerMonth
	if multiAZ {
		base *= 2
	}
	return base
}

// EstimateLBCost returns the estimated monthly cost of a load balancer.
func EstimateLBCost(lbType, state string) float64 {
	if state != "active" {
		return 0
	}
	if lbType == "network" {
		return nlbMonthlyBase
	}
	return albMonthlyBase
}

// EstimateEKSCost returns the estimated monthly control-plane cost.
func EstimateEKSCost(status string) float64 {
	if status != "ACTIVE" {
		return 0
	}
	return eksHourly * hoursPerMonth
}

// EstimateLambdaCost returns a rough monthly estimate without usage data.
func EstimateLambdaCost(state string) float64 {
	if state != "Active" {
		return 0
	}
	return lambdaMonthlyEstimate
}

// EstimateAuroraCost returns the estimated monthly cost of an Aurora cluster.
func EstimateAuroraCost(engineMode, status string, instanceCount int) float64 {
	if status != "available" && status != "backing-up" && status != "modifying" {
		return 0
	}
	if engineMode == "serverless" {
		// Aurora Serverless v2: ~0.12 USD/ACU-hour, assume 2 ACU minimum.
		return 0.12 * 2 * hoursPerMonth
	}
	if instanceCount < 1 {
		instanceCount = 1
	}
	return 0.26 * hoursPerMonth * float64(instanceCount)
}

// EstimateS3Cost returns a rough flat monthly estimate for a bucket.
func EstimateS3Cost() float64 { return s3MonthlyEstimate }

// CostBreakdown is the FinOps summary used by MCP and the web cost heatmap.
type CostBreakdown struct {
	Total     float64            `json:"total"`
	ByService map[string]float64 `json:"by_service"`
	Resources []CostEntry        `json:"resources"`
}

// CostEntry is one resource's estimated monthly cost.
type CostEntry struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Label       string  `json:"label"`
	MonthlyCost float64 `json:"monthly_cost"`
}

// SummarizeCosts aggregates estimated monthly costs from a topology graph.
func SummarizeCosts(g *graph.InfraGraph) CostBreakdown {
	out := CostBreakdown{ByService: map[string]float64{}, Resources: []CostEntry{}}
	if g == nil {
		return out
	}
	for _, r := range g.Resources() {
		out.Total += r.MonthlyCost
		out.ByService[string(r.Kind)] += r.MonthlyCost
		if r.MonthlyCost > 0 {
			out.Resources = append(out.Resources, CostEntry{
				ID: r.ID, Kind: string(r.Kind), Label: r.Label, MonthlyCost: r.MonthlyCost,
			})
		}
	}
	return out
}

// FormatCost renders a cost as a compact currency string.
func FormatCost(cost float64) string {
	switch {
	case cost == 0:
		return "Free"
	case cost < 1:
		return fmt.Sprintf("~$%.2f/mo", cost)
	case cost < 100:
		return fmt.Sprintf("~$%.0f/mo", cost)
	default:
		return fmt.Sprintf("~$%s/mo", groupThousands(cost))
	}
}

func groupThousands(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
