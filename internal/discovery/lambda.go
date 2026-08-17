package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// LambdaAPI is the subset of the Lambda client used by the discoverer.
type LambdaAPI interface {
	ListFunctions(ctx context.Context, in *lambda.ListFunctionsInput, opts ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error)
	GetFunctionConfiguration(ctx context.Context, in *lambda.GetFunctionConfigurationInput, opts ...func(*lambda.Options)) (*lambda.GetFunctionConfigurationOutput, error)
	ListTags(ctx context.Context, in *lambda.ListTagsInput, opts ...func(*lambda.Options)) (*lambda.ListTagsOutput, error)
	ListEventSourceMappings(ctx context.Context, in *lambda.ListEventSourceMappingsInput, opts ...func(*lambda.Options)) (*lambda.ListEventSourceMappingsOutput, error)
}

// LambdaDiscoverer discovers Lambda functions, their runtime state, and event
// source mappings (which become "invokes" edges when the source is in-graph).
type LambdaDiscoverer struct {
	client LambdaAPI
	opts   Options
}

// NewLambdaDiscoverer builds a Lambda discoverer.
func NewLambdaDiscoverer(client LambdaAPI, opts Options) *LambdaDiscoverer {
	return &LambdaDiscoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *LambdaDiscoverer) ServiceName() string { return "lambda" }

// Discover implements Discoverer.
func (d *LambdaDiscoverer) Discover(ctx context.Context) (*Result, error) {
	var fns []lambdatypes.FunctionConfiguration
	var marker *string
	for {
		page, err := d.client.ListFunctions(ctx, &lambda.ListFunctionsInput{Marker: marker})
		if err != nil {
			return nil, fmt.Errorf("list functions: %w", err)
		}
		fns = append(fns, page.Functions...)
		if page.NextMarker == nil {
			break
		}
		marker = page.NextMarker
	}

	res := &Result{}
	for _, fn := range fns {
		arn := aws.ToString(fn.FunctionArn)

		var vpcID string
		var subnetIDs, sgIDs []string
		if fn.VpcConfig != nil {
			vpcID = aws.ToString(fn.VpcConfig.VpcId)
			subnetIDs = fn.VpcConfig.SubnetIds
			sgIDs = fn.VpcConfig.SecurityGroupIds
		}
		if d.opts.VPCID != "" && vpcID != "" && vpcID != d.opts.VPCID {
			continue
		}

		// ListFunctions does not populate State; fetch it per function.
		state := "Unknown"
		if cfg, err := d.client.GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{
			FunctionName: aws.String(arn),
		}); err == nil {
			state = string(cfg.State)
		}

		tags := map[string]string{}
		if tagResp, err := d.client.ListTags(ctx, &lambda.ListTagsInput{Resource: aws.String(arn)}); err == nil {
			tags = tagResp.Tags
		}

		var eventSourceARNs []string
		var esMarker *string
		for {
			mappings, err := d.client.ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
				FunctionName: aws.String(arn),
				Marker:       esMarker,
			})
			if err != nil {
				break // best-effort
			}
			for _, m := range mappings.EventSourceMappings {
				if m.EventSourceArn != nil {
					eventSourceARNs = append(eventSourceARNs, *m.EventSourceArn)
				}
			}
			if mappings.NextMarker == nil {
				break
			}
			esMarker = mappings.NextMarker
		}

		health := graph.HealthUnhealthy
		if state == "Active" {
			health = graph.HealthHealthy
		}

		res.Resources = append(res.Resources, graph.Resource{
			ID:          arn,
			Kind:        graph.KindLambda,
			Label:       aws.ToString(fn.FunctionName),
			Provider:    "aws",
			Region:      d.opts.Region,
			Health:      health,
			MonthlyCost: EstimateLambdaCost(state),
			Tags:        tags,
			Attrs: map[string]any{
				"runtime":            string(fn.Runtime),
				"state":              state,
				"last_modified":      aws.ToString(fn.LastModified),
				"memory_size":        int(aws.ToInt32(fn.MemorySize)),
				"timeout":            int(aws.ToInt32(fn.Timeout)),
				"vpc_id":             vpcID,
				"subnet_ids":         subnetIDs,
				"security_group_ids": sgIDs,
				"event_source_arns":  eventSourceARNs,
			},
		})
		if vpcID != "" {
			res.ConditionalEdges = append(res.ConditionalEdges, graph.Edge{From: arn, To: vpcID, Relation: graph.RelationInVPC})
		}
		for _, sg := range sgIDs {
			res.Edges = append(res.Edges, graph.Edge{From: arn, To: sg, Relation: graph.RelationGuardedBy})
		}
		// Event sources (SQS, ALB TGs, ...) invoke the function; edge only
		// materializes when the source resource is part of the topology.
		for _, src := range eventSourceARNs {
			res.ConditionalEdges = append(res.ConditionalEdges, graph.Edge{From: src, To: arn, Relation: graph.RelationInvokes})
		}
	}
	return res, nil
}
