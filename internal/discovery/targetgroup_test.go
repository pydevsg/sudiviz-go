package discovery

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

type mockTG struct {
	tgs    []elbv2types.TargetGroup
	health map[string][]elbv2types.TargetHealthDescription
	tags   map[string][]elbv2types.Tag
}

func (m *mockTG) DescribeTargetGroups(ctx context.Context, in *elbv2.DescribeTargetGroupsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error) {
	return &elbv2.DescribeTargetGroupsOutput{TargetGroups: m.tgs}, nil
}
func (m *mockTG) DescribeTargetHealth(ctx context.Context, in *elbv2.DescribeTargetHealthInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error) {
	return &elbv2.DescribeTargetHealthOutput{TargetHealthDescriptions: m.health[aws.ToString(in.TargetGroupArn)]}, nil
}
func (m *mockTG) DescribeTags(ctx context.Context, in *elbv2.DescribeTagsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error) {
	var out []elbv2types.TagDescription
	for _, arn := range in.ResourceArns {
		out = append(out, elbv2types.TagDescription{ResourceArn: aws.String(arn), Tags: m.tags[arn]})
	}
	return &elbv2.DescribeTagsOutput{TagDescriptions: out}, nil
}

func TestTargetGroupDiscoverer(t *testing.T) {
	arn := "arn:tg"
	m := &mockTG{
		tgs: []elbv2types.TargetGroup{{
			TargetGroupArn:  aws.String(arn),
			TargetGroupName: aws.String("web"),
			Protocol:        elbv2types.ProtocolEnumHttp,
			Port:            aws.Int32(80),
			VpcId:           aws.String("vpc-1"),
			TargetType:      elbv2types.TargetTypeEnumInstance,
		}},
		health: map[string][]elbv2types.TargetHealthDescription{
			arn: {{
				Target:       &elbv2types.TargetDescription{Id: aws.String("i-1"), Port: aws.Int32(80)},
				TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
			}, {
				Target:       &elbv2types.TargetDescription{Id: aws.String("i-2"), Port: aws.Int32(80)},
				TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumUnhealthy},
			}},
		},
	}
	d := NewTargetGroupDiscoverer(m, Options{Region: "us-east-1"})
	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	var tg *graph.Resource
	for i := range res.Resources {
		if res.Resources[i].Kind == graph.KindTargetGroup {
			tg = &res.Resources[i]
		}
	}
	require.NotNil(t, tg)
	assert.Equal(t, 1, tg.AttrInt("healthy_count"))
	assert.Equal(t, 2, tg.AttrInt("total_count"))
	assert.Equal(t, graph.HealthUnhealthy, tg.Health)
	assert.Len(t, res.Edges, 2)
}

func TestTargetGroupSkipsIPTargets(t *testing.T) {
	arn := "arn:tg-ip"
	m := &mockTG{
		tgs: []elbv2types.TargetGroup{{
			TargetGroupArn: aws.String(arn), TargetGroupName: aws.String("ip"),
			Port: aws.Int32(80), TargetType: elbv2types.TargetTypeEnumIp,
		}},
		health: map[string][]elbv2types.TargetHealthDescription{
			arn: {{
				Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.0.9")},
				TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
			}},
		},
	}
	res, err := NewTargetGroupDiscoverer(m, Options{}).Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Edges)
	require.Len(t, res.Resources, 1)
	assert.Equal(t, graph.KindTargetGroup, res.Resources[0].Kind)
}
