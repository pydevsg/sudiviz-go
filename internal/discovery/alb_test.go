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

type mockELB struct {
	lbs       []elbv2types.LoadBalancer
	listeners map[string][]elbv2types.Listener
	rules     map[string][]elbv2types.Rule
	tags      map[string][]elbv2types.Tag
}

func (m *mockELB) DescribeLoadBalancers(ctx context.Context, in *elbv2.DescribeLoadBalancersInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	return &elbv2.DescribeLoadBalancersOutput{LoadBalancers: m.lbs}, nil
}
func (m *mockELB) DescribeListeners(ctx context.Context, in *elbv2.DescribeListenersInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeListenersOutput, error) {
	return &elbv2.DescribeListenersOutput{Listeners: m.listeners[aws.ToString(in.LoadBalancerArn)]}, nil
}
func (m *mockELB) DescribeRules(ctx context.Context, in *elbv2.DescribeRulesInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeRulesOutput, error) {
	return &elbv2.DescribeRulesOutput{Rules: m.rules[aws.ToString(in.ListenerArn)]}, nil
}
func (m *mockELB) DescribeTags(ctx context.Context, in *elbv2.DescribeTagsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error) {
	var out []elbv2types.TagDescription
	for _, arn := range in.ResourceArns {
		out = append(out, elbv2types.TagDescription{ResourceArn: aws.String(arn), Tags: m.tags[arn]})
	}
	return &elbv2.DescribeTagsOutput{TagDescriptions: out}, nil
}

func TestALBDiscoverer(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/app/public/abc"
	tg := "arn:aws:elasticloadbalancing:us-east-1:1:targetgroup/web/123"
	lst := "arn:aws:elasticloadbalancing:us-east-1:1:listener/app/public/abc/l1"
	m := &mockELB{
		lbs: []elbv2types.LoadBalancer{{
			LoadBalancerArn:  aws.String(arn),
			LoadBalancerName: aws.String("public"),
			DNSName:          aws.String("public.us-east-1.elb.amazonaws.com"),
			Scheme:           elbv2types.LoadBalancerSchemeEnumInternetFacing,
			Type:             elbv2types.LoadBalancerTypeEnumApplication,
			State:            &elbv2types.LoadBalancerState{Code: elbv2types.LoadBalancerStateEnumActive},
			VpcId:            aws.String("vpc-1"),
			SecurityGroups:   []string{"sg-alb"},
		}},
		listeners: map[string][]elbv2types.Listener{
			arn: {{
				ListenerArn: aws.String(lst), Port: aws.Int32(80), Protocol: elbv2types.ProtocolEnumHttp,
				DefaultActions: []elbv2types.Action{{TargetGroupArn: aws.String(tg)}},
			}},
		},
		rules: map[string][]elbv2types.Rule{lst: {}},
		tags:  map[string][]elbv2types.Tag{arn: {{Key: aws.String("Service"), Value: aws.String("web")}}},
	}
	d := NewALBDiscoverer(m, Options{Region: "us-east-1"})
	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)
	assert.Equal(t, graph.KindALB, res.Resources[0].Kind)
	assert.Equal(t, "public", res.Resources[0].Label)
	assert.InDelta(t, 22.0, res.Resources[0].MonthlyCost, 0.01)
	require.NotEmpty(t, res.Edges)
	assert.Equal(t, graph.RelationForwardsTo, res.Edges[len(res.Edges)-1].Relation)
	assert.Equal(t, tg, res.Edges[len(res.Edges)-1].To)
}

func TestALBDiscovererTagFilter(t *testing.T) {
	arn := "arn:alb"
	m := &mockELB{
		lbs: []elbv2types.LoadBalancer{{
			LoadBalancerArn: aws.String(arn), LoadBalancerName: aws.String("other"),
			State: &elbv2types.LoadBalancerState{Code: elbv2types.LoadBalancerStateEnumActive},
			VpcId: aws.String("vpc-1"),
		}},
		listeners: map[string][]elbv2types.Listener{arn: {}},
		tags:      map[string][]elbv2types.Tag{arn: {{Key: aws.String("Service"), Value: aws.String("other")}}},
	}
	d := NewALBDiscoverer(m, Options{ServiceTag: "Service=web"})
	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Resources)
}
