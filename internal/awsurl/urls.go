// Package awsurl builds AWS Console, CloudWatch, and pricing deep links.
package awsurl

import (
	"fmt"
	"strings"
)

// Console returns an AWS Console deep link for a sudiviz resource kind.
func Console(kind, resourceID, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	base := fmt.Sprintf("https://%s.console.aws.amazon.com", region)
	switch strings.ToLower(kind) {
	case "alb":
		return fmt.Sprintf("%s/ec2/home?region=%s#LoadBalancer:loadBalancerArn=%s", base, region, resourceID)
	case "target_group":
		return fmt.Sprintf("%s/ec2/home?region=%s#TargetGroup:targetGroupArn=%s", base, region, resourceID)
	case "instance":
		return fmt.Sprintf("%s/ec2/home?region=%s#InstanceDetails:instanceId=%s", base, region, resourceID)
	case "security_group":
		return fmt.Sprintf("%s/ec2/home?region=%s#SecurityGroup:groupId=%s", base, region, resourceID)
	case "vpc":
		return fmt.Sprintf("%s/vpc/home?region=%s#VpcDetails:VpcId=%s", base, region, resourceID)
	case "ecs_cluster":
		name := after(resourceID, "cluster/")
		return fmt.Sprintf("%s/ecs/v2/clusters/%s/services?region=%s", base, name, region)
	case "ecs_service":
		part := after(resourceID, "service/")
		parts := strings.Split(part, "/")
		cluster, service := "unknown", part
		if len(parts) >= 2 {
			cluster, service = parts[0], parts[len(parts)-1]
		}
		return fmt.Sprintf("%s/ecs/v2/clusters/%s/services/%s?region=%s", base, cluster, service, region)
	case "eks_cluster":
		return fmt.Sprintf("%s/eks/home?region=%s#/clusters/%s", base, region, lastSlash(resourceID))
	case "eks_nodegroup":
		parts := strings.Split(resourceID, "/")
		cluster, ng := "unknown", resourceID
		if len(parts) >= 3 {
			cluster = parts[len(parts)-3]
			ng = parts[len(parts)-2]
		}
		return fmt.Sprintf("%s/eks/home?region=%s#/clusters/%s/nodegroups/%s", base, region, cluster, ng)
	case "rds", "aurora":
		return fmt.Sprintf("%s/rds/home?region=%s#database:id=%s;is-cluster=%t", base, region, lastColon(resourceID), kind == "aurora")
	case "lambda":
		return fmt.Sprintf("%s/lambda/home?region=%s#/functions/%s", base, region, lastColon(resourceID))
	case "s3":
		bucket := strings.TrimPrefix(resourceID, "arn:aws:s3:::")
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s", bucket, region)
	default:
		return fmt.Sprintf("%s/console/home?region=%s", base, region)
	}
}

// Pricing returns a public AWS pricing page for the resource kind.
func Pricing(kind string, metadata map[string]any) string {
	switch strings.ToLower(kind) {
	case "instance":
		base := "https://aws.amazon.com/ec2/pricing/on-demand/"
		if itype, _ := metadata["instance_type"].(string); itype != "" {
			return base + "#" + itype
		}
		return base
	case "rds":
		engine, _ := metadata["engine"].(string)
		if strings.HasPrefix(engine, "aurora") {
			return "https://aws.amazon.com/rds/aurora/pricing/"
		}
		return "https://aws.amazon.com/rds/pricing/"
	case "aurora":
		return "https://aws.amazon.com/rds/aurora/pricing/"
	case "alb", "target_group":
		return "https://aws.amazon.com/elasticloadbalancing/pricing/"
	case "lambda":
		return "https://aws.amazon.com/lambda/pricing/"
	case "s3":
		return "https://aws.amazon.com/s3/pricing/"
	case "eks_cluster", "eks_nodegroup":
		return "https://aws.amazon.com/eks/pricing/"
	case "ecs_cluster", "ecs_service":
		return "https://aws.amazon.com/ecs/pricing/"
	case "security_group", "vpc":
		return "https://aws.amazon.com/vpc/pricing/"
	default:
		return "https://aws.amazon.com/pricing/"
	}
}

// Metrics returns a CloudWatch metrics URL, or "" if the kind has none.
func Metrics(kind, resourceID, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	base := fmt.Sprintf("https://%s.console.aws.amazon.com/cloudwatch/home?region=%s", region, region)
	switch strings.ToLower(kind) {
	case "instance":
		return base + "#metricsV2:graph=~();query=~'*7bAWS*2fEC2*2cInstanceId*7d*20" + resourceID
	case "alb":
		return base + "#metricsV2:graph=~();query=~'*7bAWS*2fApplicationELB*2cLoadBalancer*7d*20" + after(resourceID, "loadbalancer/")
	case "target_group":
		return base + "#metricsV2:graph=~();query=~'*7bAWS*2fApplicationELB*2cTargetGroup*7d*20" + after(resourceID, "targetgroup/")
	case "rds":
		return base + "#metricsV2:graph=~();query=~'*7bAWS*2fRDS*2cDBInstanceIdentifier*7d*20" + lastColon(resourceID)
	case "lambda":
		return base + "#metricsV2:graph=~();query=~'*7bAWS*2fLambda*2cFunctionName*7d*20" + lastColon(resourceID)
	case "ecs_service":
		part := after(resourceID, "service/")
		parts := strings.Split(part, "/")
		cluster, service := "unknown", part
		if len(parts) >= 2 {
			cluster, service = parts[0], parts[len(parts)-1]
		}
		return base + "#metricsV2:graph=~();query=~'*7bAWS*2fECS*2cClusterName*2cServiceName*7d*20" + cluster + "*20" + service
	case "ecs_cluster":
		return base + "#metricsV2:graph=~();query=~'*7bAWS*2fECS*2cClusterName*7d*20" + after(resourceID, "cluster/")
	default:
		return ""
	}
}

// Logs returns a CloudWatch Logs URL, or "" if the kind has no standard log group.
func Logs(kind, resourceID, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	base := fmt.Sprintf("https://%s.console.aws.amazon.com/cloudwatch/home?region=%s", region, region)
	encode := func(g string) string { return strings.ReplaceAll(g, "/", "*2f") }
	switch strings.ToLower(kind) {
	case "lambda":
		return base + "#logsV2:log-groups/log-group/" + encode("/aws/lambda/"+lastColon(resourceID))
	case "ecs_service":
		part := after(resourceID, "service/")
		parts := strings.Split(part, "/")
		return base + "#logsV2:log-groups/log-group/" + encode("/ecs/"+parts[len(parts)-1])
	case "rds":
		return base + "#logsV2:log-groups$3FlogGroupNameFilter$3D" + encode("/aws/rds/instance/"+lastColon(resourceID))
	case "eks_cluster":
		return base + "#logsV2:log-groups$3FlogGroupNameFilter$3D" + encode("/aws/eks/"+lastSlash(resourceID))
	default:
		return ""
	}
}

func after(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return lastSlash(s)
}

func lastSlash(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func lastColon(s string) string {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s
}
