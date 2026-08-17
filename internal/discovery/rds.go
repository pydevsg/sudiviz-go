package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// RDSAPI is the subset of the RDS client used by the RDS/Aurora discoverer.
type RDSAPI interface {
	DescribeDBInstances(ctx context.Context, in *rds.DescribeDBInstancesInput, opts ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
	DescribeDBClusters(ctx context.Context, in *rds.DescribeDBClustersInput, opts ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error)
	ListTagsForResource(ctx context.Context, in *rds.ListTagsForResourceInput, opts ...func(*rds.Options)) (*rds.ListTagsForResourceOutput, error)
}

// RDSDiscoverer discovers RDS instances and Aurora clusters. Non-Aurora
// engines come from DescribeDBInstances; DescribeDBClusters covers Aurora
// (engines prefixed "aurora") so there is no overlap.
type RDSDiscoverer struct {
	client RDSAPI
	opts   Options
}

// NewRDSDiscoverer builds an RDS/Aurora discoverer.
func NewRDSDiscoverer(client RDSAPI, opts Options) *RDSDiscoverer {
	return &RDSDiscoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *RDSDiscoverer) ServiceName() string { return "rds" }

// Discover implements Discoverer.
func (d *RDSDiscoverer) Discover(ctx context.Context) (*Result, error) {
	res := &Result{}
	if err := d.discoverInstances(ctx, res); err != nil {
		return nil, err
	}
	if err := d.discoverAuroraClusters(ctx, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (d *RDSDiscoverer) discoverInstances(ctx context.Context, res *Result) error {
	var marker *string
	for {
		page, err := d.client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{Marker: marker})
		if err != nil {
			return fmt.Errorf("describe db instances: %w", err)
		}
		for _, db := range page.DBInstances {
			var vpcID, subnetGroup string
			if db.DBSubnetGroup != nil {
				vpcID = aws.ToString(db.DBSubnetGroup.VpcId)
				subnetGroup = aws.ToString(db.DBSubnetGroup.DBSubnetGroupName)
			}
			if d.opts.VPCID != "" && vpcID != "" && vpcID != d.opts.VPCID {
				continue
			}

			arn := aws.ToString(db.DBInstanceArn)
			status := aws.ToString(db.DBInstanceStatus)
			health := graph.HealthUnhealthy
			if status == "available" {
				health = graph.HealthHealthy
			}
			var sgIDs []string
			for _, sg := range db.VpcSecurityGroups {
				if sg.VpcSecurityGroupId != nil {
					sgIDs = append(sgIDs, *sg.VpcSecurityGroupId)
				}
			}
			var endpointAddr string
			endpointPort := 0
			if db.Endpoint != nil {
				endpointAddr = aws.ToString(db.Endpoint.Address)
				endpointPort = int(aws.ToInt32(db.Endpoint.Port))
			}
			instanceClass := aws.ToString(db.DBInstanceClass)
			multiAZ := aws.ToBool(db.MultiAZ)

			res.Resources = append(res.Resources, graph.Resource{
				ID:          arn,
				Kind:        graph.KindRDS,
				Label:       aws.ToString(db.DBInstanceIdentifier),
				Provider:    "aws",
				Region:      d.opts.Region,
				AZ:          aws.ToString(db.AvailabilityZone),
				Health:      health,
				MonthlyCost: EstimateRDSCost(instanceClass, status, multiAZ),
				Tags:        d.resourceTags(ctx, arn),
				Attrs: map[string]any{
					"engine":              aws.ToString(db.Engine),
					"engine_version":      aws.ToString(db.EngineVersion),
					"db_instance_class":   instanceClass,
					"status":              status,
					"endpoint_address":    endpointAddr,
					"endpoint_port":       endpointPort,
					"vpc_id":              vpcID,
					"subnet_group":        subnetGroup,
					"security_group_ids":  sgIDs,
					"multi_az":            multiAZ,
					"publicly_accessible": aws.ToBool(db.PubliclyAccessible),
					"storage_encrypted":   aws.ToBool(db.StorageEncrypted),
				},
			})
			if vpcID != "" {
				res.ConditionalEdges = append(res.ConditionalEdges, graph.Edge{From: arn, To: vpcID, Relation: graph.RelationInVPC})
			}
			for _, sg := range sgIDs {
				res.Edges = append(res.Edges, graph.Edge{From: arn, To: sg, Relation: graph.RelationGuardedBy})
			}
		}
		if page.Marker == nil {
			break
		}
		marker = page.Marker
	}
	return nil
}

func (d *RDSDiscoverer) discoverAuroraClusters(ctx context.Context, res *Result) error {
	var marker *string
	for {
		page, err := d.client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{Marker: marker})
		if err != nil {
			return fmt.Errorf("describe db clusters: %w", err)
		}
		for _, cluster := range page.DBClusters {
			engine := aws.ToString(cluster.Engine)
			if !strings.HasPrefix(engine, "aurora") {
				continue
			}
			vpcID := "" // DBClusters output does not carry the VPC directly
			arn := aws.ToString(cluster.DBClusterArn)
			status := aws.ToString(cluster.Status)
			health := graph.HealthUnhealthy
			if status == "available" {
				health = graph.HealthHealthy
			}
			engineMode := aws.ToString(cluster.EngineMode)
			if engineMode == "" {
				engineMode = "provisioned"
			}
			var sgIDs []string
			for _, sg := range cluster.VpcSecurityGroups {
				if sg.VpcSecurityGroupId != nil {
					sgIDs = append(sgIDs, *sg.VpcSecurityGroupId)
				}
			}
			instanceCount := len(cluster.DBClusterMembers)

			res.Resources = append(res.Resources, graph.Resource{
				ID:          arn,
				Kind:        graph.KindAurora,
				Label:       aws.ToString(cluster.DBClusterIdentifier),
				Provider:    "aws",
				Region:      d.opts.Region,
				Health:      health,
				MonthlyCost: EstimateAuroraCost(engineMode, status, instanceCount),
				Tags:        d.resourceTags(ctx, arn),
				Attrs: map[string]any{
					"engine":              engine,
					"engine_version":      aws.ToString(cluster.EngineVersion),
					"engine_mode":         engineMode,
					"status":              status,
					"endpoint":            aws.ToString(cluster.Endpoint),
					"reader_endpoint":     aws.ToString(cluster.ReaderEndpoint),
					"port":                int(aws.ToInt32(cluster.Port)),
					"vpc_id":              vpcID,
					"security_group_ids":  sgIDs,
					"multi_az":            aws.ToBool(cluster.MultiAZ),
					"storage_encrypted":   aws.ToBool(cluster.StorageEncrypted),
					"deletion_protection": aws.ToBool(cluster.DeletionProtection),
					"instance_count":      instanceCount,
				},
			})
			for _, sg := range sgIDs {
				res.Edges = append(res.Edges, graph.Edge{From: arn, To: sg, Relation: graph.RelationGuardedBy})
			}
		}
		if page.Marker == nil {
			break
		}
		marker = page.Marker
	}
	return nil
}

// resourceTags fetches RDS tags best-effort.
func (d *RDSDiscoverer) resourceTags(ctx context.Context, arn string) map[string]string {
	tags := map[string]string{}
	resp, err := d.client.ListTagsForResource(ctx, &rds.ListTagsForResourceInput{ResourceName: aws.String(arn)})
	if err != nil {
		return tags
	}
	for _, t := range resp.TagList {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return tags
}
