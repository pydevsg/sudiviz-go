package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// ECSAPI is the subset of the ECS client used by the ECS discoverer.
type ECSAPI interface {
	ListClusters(ctx context.Context, in *ecs.ListClustersInput, opts ...func(*ecs.Options)) (*ecs.ListClustersOutput, error)
	DescribeClusters(ctx context.Context, in *ecs.DescribeClustersInput, opts ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error)
	ListServices(ctx context.Context, in *ecs.ListServicesInput, opts ...func(*ecs.Options)) (*ecs.ListServicesOutput, error)
	DescribeServices(ctx context.Context, in *ecs.DescribeServicesInput, opts ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
}

// ECSDiscoverer discovers ECS clusters, services (with task counts), and the
// service→target-group / service→SG relationships.
type ECSDiscoverer struct {
	client ECSAPI
	opts   Options
}

// NewECSDiscoverer builds an ECS discoverer.
func NewECSDiscoverer(client ECSAPI, opts Options) *ECSDiscoverer {
	return &ECSDiscoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *ECSDiscoverer) ServiceName() string { return "ecs" }

// Discover implements Discoverer.
func (d *ECSDiscoverer) Discover(ctx context.Context) (*Result, error) {
	var clusterARNs []string
	var token *string
	for {
		page, err := d.client.ListClusters(ctx, &ecs.ListClustersInput{NextToken: token})
		if err != nil {
			return nil, fmt.Errorf("list clusters: %w", err)
		}
		clusterARNs = append(clusterARNs, page.ClusterArns...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	if len(clusterARNs) == 0 {
		return &Result{}, nil
	}

	res := &Result{}
	for i := 0; i < len(clusterARNs); i += 100 {
		end := min(i+100, len(clusterARNs))
		resp, err := d.client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: clusterARNs[i:end],
			Include:  []ecstypes.ClusterField{ecstypes.ClusterFieldTags},
		})
		if err != nil {
			return nil, fmt.Errorf("describe clusters: %w", err)
		}
		for _, cluster := range resp.Clusters {
			if err := d.appendCluster(ctx, cluster, res); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

func (d *ECSDiscoverer) appendCluster(ctx context.Context, cluster ecstypes.Cluster, res *Result) error {
	arn := aws.ToString(cluster.ClusterArn)
	status := aws.ToString(cluster.Status)
	health := graph.HealthUnknown
	if status == "ACTIVE" {
		health = graph.HealthHealthy
	}
	tags := map[string]string{}
	for _, t := range cluster.Tags {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}

	res.Resources = append(res.Resources, graph.Resource{
		ID:       arn,
		Kind:     graph.KindECSCluster,
		Label:    aws.ToString(cluster.ClusterName),
		Provider: "aws",
		Region:   d.opts.Region,
		Health:   health,
		Tags:     tags,
		Attrs: map[string]any{
			"status":                    status,
			"active_services_count":     int(cluster.ActiveServicesCount),
			"running_tasks_count":       int(cluster.RunningTasksCount),
			"pending_tasks_count":       int(cluster.PendingTasksCount),
			"container_instances_count": int(cluster.RegisteredContainerInstancesCount),
			"capacity_providers":        cluster.CapacityProviders,
		},
	})
	return d.appendServices(ctx, arn, res)
}

func (d *ECSDiscoverer) appendServices(ctx context.Context, clusterARN string, res *Result) error {
	var serviceARNs []string
	var token *string
	for {
		page, err := d.client.ListServices(ctx, &ecs.ListServicesInput{
			Cluster:   aws.String(clusterARN),
			NextToken: token,
		})
		if err != nil {
			return fmt.Errorf("list services for %s: %w", clusterARN, err)
		}
		serviceARNs = append(serviceARNs, page.ServiceArns...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	for i := 0; i < len(serviceARNs); i += 10 {
		end := min(i+10, len(serviceARNs))
		resp, err := d.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  aws.String(clusterARN),
			Services: serviceARNs[i:end],
			Include:  []ecstypes.ServiceField{ecstypes.ServiceFieldTags},
		})
		if err != nil {
			return fmt.Errorf("describe services for %s: %w", clusterARN, err)
		}
		for _, svc := range resp.Services {
			d.appendService(clusterARN, svc, res)
		}
	}
	return nil
}

func (d *ECSDiscoverer) appendService(clusterARN string, svc ecstypes.Service, res *Result) {
	arn := aws.ToString(svc.ServiceArn)
	running := int(svc.RunningCount)
	desired := int(svc.DesiredCount)
	health := graph.HealthUnhealthy
	if running >= desired && desired > 0 {
		health = graph.HealthHealthy
	}

	var subnets, sgIDs []string
	if svc.NetworkConfiguration != nil && svc.NetworkConfiguration.AwsvpcConfiguration != nil {
		subnets = svc.NetworkConfiguration.AwsvpcConfiguration.Subnets
		sgIDs = svc.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups
	}
	var tgARNs []string
	for _, lb := range svc.LoadBalancers {
		if lb.TargetGroupArn != nil {
			tgARNs = append(tgARNs, *lb.TargetGroupArn)
		}
	}
	tags := map[string]string{}
	for _, t := range svc.Tags {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	launchType := string(svc.LaunchType)
	if launchType == "" {
		launchType = "EC2"
	}

	res.Resources = append(res.Resources, graph.Resource{
		ID:       arn,
		Kind:     graph.KindECSService,
		Label:    aws.ToString(svc.ServiceName),
		Provider: "aws",
		Region:   d.opts.Region,
		Health:   health,
		Tags:     tags,
		Attrs: map[string]any{
			"status":             aws.ToString(svc.Status),
			"desired_count":      desired,
			"running_count":      running,
			"pending_count":      int(svc.PendingCount),
			"launch_type":        launchType,
			"task_definition":    aws.ToString(svc.TaskDefinition),
			"subnet_ids":         subnets,
			"security_group_ids": sgIDs,
			"target_group_arns":  tgARNs,
		},
	})
	res.Edges = append(res.Edges, graph.Edge{From: clusterARN, To: arn, Relation: graph.RelationContains})
	// backed_by only materializes when the target group was discovered.
	for _, tg := range tgARNs {
		res.ConditionalEdges = append(res.ConditionalEdges, graph.Edge{From: arn, To: tg, Relation: graph.RelationBackedBy})
	}
	for _, sg := range sgIDs {
		res.Edges = append(res.Edges, graph.Edge{From: arn, To: sg, Relation: graph.RelationGuardedBy})
	}
}
