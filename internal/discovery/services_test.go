package discovery

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

type mockECS struct {
	clusters []string
	desc     []ecstypes.Cluster
	services []string
	svcDesc  []ecstypes.Service
}

func (m *mockECS) ListClusters(ctx context.Context, in *ecs.ListClustersInput, _ ...func(*ecs.Options)) (*ecs.ListClustersOutput, error) {
	return &ecs.ListClustersOutput{ClusterArns: m.clusters}, nil
}
func (m *mockECS) DescribeClusters(ctx context.Context, in *ecs.DescribeClustersInput, _ ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
	return &ecs.DescribeClustersOutput{Clusters: m.desc}, nil
}
func (m *mockECS) ListServices(ctx context.Context, in *ecs.ListServicesInput, _ ...func(*ecs.Options)) (*ecs.ListServicesOutput, error) {
	return &ecs.ListServicesOutput{ServiceArns: m.services}, nil
}
func (m *mockECS) DescribeServices(ctx context.Context, in *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	return &ecs.DescribeServicesOutput{Services: m.svcDesc}, nil
}

func TestECSDiscoverer(t *testing.T) {
	m := &mockECS{
		clusters: []string{"arn:cluster"},
		desc:     []ecstypes.Cluster{{ClusterArn: aws.String("arn:cluster"), ClusterName: aws.String("prod"), Status: aws.String("ACTIVE")}},
		services: []string{"arn:svc"},
		svcDesc: []ecstypes.Service{{
			ServiceArn: aws.String("arn:svc"), ServiceName: aws.String("api"),
			RunningCount: 1, DesiredCount: 1,
			LoadBalancers: []ecstypes.LoadBalancer{{TargetGroupArn: aws.String("arn:tg")}},
		}},
	}
	res, err := NewECSDiscoverer(m, Options{}).Discover(context.Background())
	require.NoError(t, err)
	assert.Len(t, res.Resources, 2)
	assert.Equal(t, graph.RelationContains, res.Edges[0].Relation)
	require.Len(t, res.ConditionalEdges, 1)
	assert.Equal(t, graph.RelationBackedBy, res.ConditionalEdges[0].Relation)
}

type mockEKS struct{}

func (mockEKS) ListClusters(ctx context.Context, in *eks.ListClustersInput, _ ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	return &eks.ListClustersOutput{Clusters: []string{"prod"}}, nil
}
func (mockEKS) DescribeCluster(ctx context.Context, in *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{
		Name: aws.String("prod"), Arn: aws.String("arn:eks"), Status: ekstypes.ClusterStatusActive,
		Version: aws.String("1.29"),
	}}, nil
}
func (mockEKS) ListNodegroups(ctx context.Context, in *eks.ListNodegroupsInput, _ ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
	return &eks.ListNodegroupsOutput{Nodegroups: []string{"ng"}}, nil
}
func (mockEKS) DescribeNodegroup(ctx context.Context, in *eks.DescribeNodegroupInput, _ ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
	return &eks.DescribeNodegroupOutput{Nodegroup: &ekstypes.Nodegroup{
		NodegroupName: aws.String("ng"), NodegroupArn: aws.String("arn:ng"),
		Status:        ekstypes.NodegroupStatusActive,
		ScalingConfig: &ekstypes.NodegroupScalingConfig{DesiredSize: aws.Int32(2), MinSize: aws.Int32(1), MaxSize: aws.Int32(3)},
	}}, nil
}

func TestEKSDiscoverer(t *testing.T) {
	res, err := NewEKSDiscoverer(mockEKS{}, Options{}).Discover(context.Background())
	require.NoError(t, err)
	assert.Len(t, res.Resources, 2)
	assert.Equal(t, graph.KindEKSCluster, res.Resources[0].Kind)
	assert.Equal(t, graph.HealthHealthy, res.Resources[0].Health)
}

type mockRDS struct {
	instances []rdstypes.DBInstance
}

func (m *mockRDS) DescribeDBInstances(ctx context.Context, in *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	return &rds.DescribeDBInstancesOutput{DBInstances: m.instances}, nil
}
func (m *mockRDS) DescribeDBClusters(ctx context.Context, in *rds.DescribeDBClustersInput, _ ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error) {
	return &rds.DescribeDBClustersOutput{}, nil
}
func (m *mockRDS) ListTagsForResource(ctx context.Context, in *rds.ListTagsForResourceInput, _ ...func(*rds.Options)) (*rds.ListTagsForResourceOutput, error) {
	return &rds.ListTagsForResourceOutput{}, nil
}

func TestRDSDiscoverer(t *testing.T) {
	m := &mockRDS{instances: []rdstypes.DBInstance{{
		DBInstanceArn: aws.String("arn:db"), DBInstanceIdentifier: aws.String("prod"),
		DBInstanceStatus: aws.String("available"), DBInstanceClass: aws.String("db.t3.micro"),
		PubliclyAccessible: aws.Bool(true), StorageEncrypted: aws.Bool(false),
		Engine: aws.String("postgres"),
	}}}
	res, err := NewRDSDiscoverer(m, Options{}).Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)
	assert.True(t, res.Resources[0].AttrBool("publicly_accessible"))
	assert.False(t, res.Resources[0].AttrBool("storage_encrypted"))
}

type mockLambda struct{}

func (mockLambda) ListFunctions(ctx context.Context, in *lambda.ListFunctionsInput, _ ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error) {
	return &lambda.ListFunctionsOutput{Functions: []lambdatypes.FunctionConfiguration{{
		FunctionArn: aws.String("arn:fn"), FunctionName: aws.String("worker"), Runtime: lambdatypes.RuntimeGo1x,
	}}}, nil
}
func (mockLambda) GetFunctionConfiguration(ctx context.Context, in *lambda.GetFunctionConfigurationInput, _ ...func(*lambda.Options)) (*lambda.GetFunctionConfigurationOutput, error) {
	return &lambda.GetFunctionConfigurationOutput{State: lambdatypes.StateActive}, nil
}
func (mockLambda) ListTags(ctx context.Context, in *lambda.ListTagsInput, _ ...func(*lambda.Options)) (*lambda.ListTagsOutput, error) {
	return &lambda.ListTagsOutput{Tags: map[string]string{}}, nil
}
func (mockLambda) ListEventSourceMappings(ctx context.Context, in *lambda.ListEventSourceMappingsInput, _ ...func(*lambda.Options)) (*lambda.ListEventSourceMappingsOutput, error) {
	return &lambda.ListEventSourceMappingsOutput{EventSourceMappings: []lambdatypes.EventSourceMappingConfiguration{{
		EventSourceArn: aws.String("arn:sqs"),
	}}}, nil
}

func TestLambdaDiscoverer(t *testing.T) {
	res, err := NewLambdaDiscoverer(mockLambda{}, Options{}).Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)
	assert.Equal(t, graph.HealthHealthy, res.Resources[0].Health)
	require.Len(t, res.ConditionalEdges, 1)
	assert.Equal(t, graph.RelationInvokes, res.ConditionalEdges[0].Relation)
}
