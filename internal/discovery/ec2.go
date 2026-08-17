package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// EC2API is the subset of the EC2 client used by the instance discoverer.
type EC2API interface {
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, opts ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeVolumes(ctx context.Context, in *ec2.DescribeVolumesInput, opts ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
}

// EC2Discoverer discovers EC2 instances, their security-group attachments,
// and EBS volume encryption state.
type EC2Discoverer struct {
	client EC2API
	opts   Options
}

// NewEC2Discoverer builds an EC2 instance discoverer.
func NewEC2Discoverer(client EC2API, opts Options) *EC2Discoverer {
	return &EC2Discoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *EC2Discoverer) ServiceName() string { return "ec2" }

// Discover implements Discoverer.
func (d *EC2Discoverer) Discover(ctx context.Context) (*Result, error) {
	var filters []ec2types.Filter
	if d.opts.VPCID != "" {
		filters = append(filters, ec2types.Filter{Name: aws.String("vpc-id"), Values: []string{d.opts.VPCID}})
	}
	tagFilter := d.opts.TagFilter()

	res := &Result{}
	var instances []ec2types.Instance
	var token *string
	for {
		page, err := d.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters:   filters,
			NextToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("describe instances: %w", err)
		}
		for _, reservation := range page.Reservations {
			instances = append(instances, reservation.Instances...)
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	ebsEncrypted, err := d.volumeEncryption(ctx)
	if err != nil {
		// EBS encryption info is an enrichment; discovery proceeds without it.
		ebsEncrypted = nil
	}

	for _, inst := range instances {
		state := "unknown"
		if inst.State != nil {
			state = string(inst.State.Name)
		}
		// Terminated instances are no longer part of the live topology.
		if state == "terminated" {
			continue
		}

		tags := map[string]string{}
		for _, t := range inst.Tags {
			tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
		}
		if !matchesTags(tags, tagFilter) {
			continue
		}

		id := aws.ToString(inst.InstanceId)
		label := tags["Name"]
		if label == "" {
			label = id
		}
		health := graph.HealthUnhealthy
		if state == "running" {
			health = graph.HealthHealthy
		}

		var sgIDs []string
		for _, sg := range inst.SecurityGroups {
			sgIDs = append(sgIDs, aws.ToString(sg.GroupId))
		}

		// Aggregate EBS encryption: encrypted only if every attached volume is.
		allEncrypted := true
		volumesSeen := false
		for _, bdm := range inst.BlockDeviceMappings {
			if bdm.Ebs == nil || bdm.Ebs.VolumeId == nil {
				continue
			}
			if enc, ok := ebsEncrypted[*bdm.Ebs.VolumeId]; ok {
				volumesSeen = true
				if !enc {
					allEncrypted = false
				}
			}
		}
		instanceType := string(inst.InstanceType)

		res.Resources = append(res.Resources, graph.Resource{
			ID:          id,
			Kind:        graph.KindInstance,
			Label:       label,
			Provider:    "aws",
			Region:      d.opts.Region,
			AZ:          placementAZ(inst),
			Health:      health,
			MonthlyCost: EstimateInstanceCost(instanceType, state),
			Tags:        tags,
			Attrs: map[string]any{
				"instance_type":      instanceType,
				"state":              state,
				"private_ip":         aws.ToString(inst.PrivateIpAddress),
				"public_ip":          aws.ToString(inst.PublicIpAddress),
				"vpc_id":             aws.ToString(inst.VpcId),
				"subnet_id":          aws.ToString(inst.SubnetId),
				"security_group_ids": sgIDs,
				"ebs_encrypted":      volumesSeen && allEncrypted,
				"ebs_checked":        volumesSeen,
			},
		})
		for _, sg := range sgIDs {
			res.Edges = append(res.Edges, graph.Edge{From: id, To: sg, Relation: graph.RelationGuardedBy})
		}
	}
	return res, nil
}

// volumeEncryption maps volume ID → encrypted flag for the account/region.
func (d *EC2Discoverer) volumeEncryption(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	var token *string
	for {
		page, err := d.client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, v := range page.Volumes {
			out[aws.ToString(v.VolumeId)] = aws.ToBool(v.Encrypted)
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func placementAZ(inst ec2types.Instance) string {
	if inst.Placement == nil {
		return ""
	}
	return aws.ToString(inst.Placement.AvailabilityZone)
}
