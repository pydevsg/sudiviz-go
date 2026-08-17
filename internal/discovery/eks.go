package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// EKSAPI is the subset of the EKS client used by the EKS discoverer.
type EKSAPI interface {
	ListClusters(ctx context.Context, in *eks.ListClustersInput, opts ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(ctx context.Context, in *eks.DescribeClusterInput, opts ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	ListNodegroups(ctx context.Context, in *eks.ListNodegroupsInput, opts ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error)
	DescribeNodegroup(ctx context.Context, in *eks.DescribeNodegroupInput, opts ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
}

// EKSDiscoverer discovers EKS clusters and their managed node groups.
type EKSDiscoverer struct {
	client EKSAPI
	opts   Options
}

// NewEKSDiscoverer builds an EKS discoverer.
func NewEKSDiscoverer(client EKSAPI, opts Options) *EKSDiscoverer {
	return &EKSDiscoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *EKSDiscoverer) ServiceName() string { return "eks" }

// Discover implements Discoverer.
func (d *EKSDiscoverer) Discover(ctx context.Context) (*Result, error) {
	var names []string
	var token *string
	for {
		page, err := d.client.ListClusters(ctx, &eks.ListClustersInput{NextToken: token})
		if err != nil {
			return nil, fmt.Errorf("list clusters: %w", err)
		}
		names = append(names, page.Clusters...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	res := &Result{}
	for _, name := range names {
		out, err := d.client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
		if err != nil || out.Cluster == nil {
			continue // best-effort per cluster
		}
		cluster := out.Cluster

		var vpcID string
		var subnetIDs, sgIDs []string
		if cluster.ResourcesVpcConfig != nil {
			vpcID = aws.ToString(cluster.ResourcesVpcConfig.VpcId)
			subnetIDs = cluster.ResourcesVpcConfig.SubnetIds
			sgIDs = cluster.ResourcesVpcConfig.SecurityGroupIds
		}
		if d.opts.VPCID != "" && vpcID != "" && vpcID != d.opts.VPCID {
			continue
		}

		arn := aws.ToString(cluster.Arn)
		if arn == "" {
			arn = fmt.Sprintf("arn:aws:eks:::%s", name)
		}
		status := string(cluster.Status)
		health := graph.HealthUnknown
		if status == "ACTIVE" {
			health = graph.HealthHealthy
		}

		res.Resources = append(res.Resources, graph.Resource{
			ID:          arn,
			Kind:        graph.KindEKSCluster,
			Label:       name,
			Provider:    "aws",
			Region:      d.opts.Region,
			Health:      health,
			MonthlyCost: EstimateEKSCost(status),
			Tags:        cluster.Tags,
			Attrs: map[string]any{
				"status":             status,
				"version":            aws.ToString(cluster.Version),
				"endpoint":           aws.ToString(cluster.Endpoint),
				"vpc_id":             vpcID,
				"subnet_ids":         subnetIDs,
				"security_group_ids": sgIDs,
			},
		})
		if vpcID != "" {
			res.ConditionalEdges = append(res.ConditionalEdges, graph.Edge{From: arn, To: vpcID, Relation: graph.RelationInVPC})
		}
		for _, sg := range sgIDs {
			res.Edges = append(res.Edges, graph.Edge{From: arn, To: sg, Relation: graph.RelationGuardedBy})
		}
		if err := d.appendNodeGroups(ctx, name, arn, res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func (d *EKSDiscoverer) appendNodeGroups(ctx context.Context, clusterName, clusterARN string, res *Result) error {
	var ngNames []string
	var token *string
	for {
		page, err := d.client.ListNodegroups(ctx, &eks.ListNodegroupsInput{
			ClusterName: aws.String(clusterName),
			NextToken:   token,
		})
		if err != nil {
			return fmt.Errorf("list nodegroups for %s: %w", clusterName, err)
		}
		ngNames = append(ngNames, page.Nodegroups...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	for _, ngName := range ngNames {
		out, err := d.client.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
			ClusterName:   aws.String(clusterName),
			NodegroupName: aws.String(ngName),
		})
		if err != nil || out.Nodegroup == nil {
			continue // best-effort per node group
		}
		ng := out.Nodegroup

		arn := aws.ToString(ng.NodegroupArn)
		if arn == "" {
			arn = fmt.Sprintf("arn:aws:eks:::%s/%s", clusterName, ngName)
		}
		status := string(ng.Status)
		health := graph.HealthUnknown
		if status == "ACTIVE" {
			health = graph.HealthHealthy
		}
		desired, minSize, maxSize := 0, 0, 0
		if ng.ScalingConfig != nil {
			desired = int(aws.ToInt32(ng.ScalingConfig.DesiredSize))
			minSize = int(aws.ToInt32(ng.ScalingConfig.MinSize))
			maxSize = int(aws.ToInt32(ng.ScalingConfig.MaxSize))
		}
		capacityType := string(ng.CapacityType)
		if capacityType == "" {
			capacityType = "ON_DEMAND"
		}

		res.Resources = append(res.Resources, graph.Resource{
			ID:       arn,
			Kind:     graph.KindEKSNodeGroup,
			Label:    ngName,
			Provider: "aws",
			Region:   d.opts.Region,
			Health:   health,
			Tags:     ng.Tags,
			Attrs: map[string]any{
				"status":         status,
				"cluster_name":   clusterName,
				"capacity_type":  capacityType,
				"instance_types": ng.InstanceTypes,
				"desired_size":   desired,
				"min_size":       minSize,
				"max_size":       maxSize,
			},
		})
		res.Edges = append(res.Edges, graph.Edge{From: clusterARN, To: arn, Relation: graph.RelationContains})
	}
	return nil
}
