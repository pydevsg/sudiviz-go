package discovery

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// SecurityGroupAPI is the subset of the EC2 client used by the SG discoverer.
type SecurityGroupAPI interface {
	DescribeSecurityGroups(ctx context.Context, in *ec2.DescribeSecurityGroupsInput, opts ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeNetworkInterfaces(ctx context.Context, in *ec2.DescribeNetworkInterfacesInput, opts ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error)
}

// SecurityGroupDiscoverer discovers security groups, their ingress/egress
// rules (including group-to-group references), and ENI attachments.
type SecurityGroupDiscoverer struct {
	client SecurityGroupAPI
	opts   Options
}

// NewSecurityGroupDiscoverer builds a security-group discoverer.
func NewSecurityGroupDiscoverer(client SecurityGroupAPI, opts Options) *SecurityGroupDiscoverer {
	return &SecurityGroupDiscoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *SecurityGroupDiscoverer) ServiceName() string { return "security_group" }

// Discover implements Discoverer.
func (d *SecurityGroupDiscoverer) Discover(ctx context.Context) (*Result, error) {
	var filters []ec2types.Filter
	if d.opts.VPCID != "" {
		filters = append(filters, ec2types.Filter{Name: aws.String("vpc-id"), Values: []string{d.opts.VPCID}})
	}

	var sgs []ec2types.SecurityGroup
	var token *string
	for {
		page, err := d.client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters:   filters,
			NextToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("describe security groups: %w", err)
		}
		sgs = append(sgs, page.SecurityGroups...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	attachments, err := d.eniAttachments(ctx)
	if err != nil {
		return nil, err
	}

	res := &Result{}
	for _, sg := range sgs {
		// The AWS default SG is not user-managed; its self-referential rules
		// clutter the graph without adding insight.
		if aws.ToString(sg.GroupName) == "default" {
			continue
		}
		sgID := aws.ToString(sg.GroupId)
		rules := flattenSGPermissions(sg.IpPermissions, "ingress")
		rules = append(rules, flattenSGPermissions(sg.IpPermissionsEgress, "egress")...)

		ingress, egress := 0, 0
		for _, r := range rules {
			if r.Direction == "ingress" {
				ingress++
			} else {
				egress++
			}
		}

		tags := map[string]string{}
		for _, t := range sg.Tags {
			tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
		}
		name := aws.ToString(sg.GroupName)
		if name == "" {
			name = sgID
		}

		attached := attachments[sgID]
		sort.Strings(attached)

		res.Resources = append(res.Resources, graph.Resource{
			ID:       sgID,
			Kind:     graph.KindSecurityGroup,
			Label:    name,
			Provider: "aws",
			Region:   d.opts.Region,
			Health:   graph.HealthHealthy,
			Tags:     tags,
			Attrs: map[string]any{
				"vpc_id":        aws.ToString(sg.VpcId),
				"rules":         rules,
				"attached_to":   attached,
				"ingress_count": ingress,
				"egress_count":  egress,
			},
		})

		// SG-to-SG reference edges visualize allowed traffic flows.
		// Ingress: traffic flows FROM the referenced SG INTO this SG;
		// egress: the reverse. Self-loops are skipped.
		for _, rule := range rules {
			for _, ref := range rule.ReferencedSGIDs {
				if ref == sgID {
					continue
				}
				attrs := map[string]any{
					"protocol":  rule.Protocol,
					"from_port": int32Val(rule.FromPort),
					"to_port":   int32Val(rule.ToPort),
					"direction": rule.Direction,
				}
				if rule.Direction == "ingress" {
					res.Edges = append(res.Edges, graph.Edge{From: ref, To: sgID, Relation: graph.RelationAllowsIngress, Attrs: attrs})
				} else {
					res.Edges = append(res.Edges, graph.Edge{From: sgID, To: ref, Relation: graph.RelationAllowsEgress, Attrs: attrs})
				}
			}
		}
	}
	return res, nil
}

// eniAttachments maps SG ID → ENI IDs it is attached to.
func (d *SecurityGroupDiscoverer) eniAttachments(ctx context.Context) (map[string][]string, error) {
	var filters []ec2types.Filter
	if d.opts.VPCID != "" {
		filters = append(filters, ec2types.Filter{Name: aws.String("vpc-id"), Values: []string{d.opts.VPCID}})
	}
	out := map[string][]string{}
	var token *string
	for {
		page, err := d.client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
			Filters:   filters,
			NextToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("describe network interfaces: %w", err)
		}
		for _, eni := range page.NetworkInterfaces {
			for _, grp := range eni.Groups {
				gid := aws.ToString(grp.GroupId)
				out[gid] = append(out[gid], aws.ToString(eni.NetworkInterfaceId))
			}
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

// flattenSGPermissions converts AWS IpPermissions into SGRule entries.
func flattenSGPermissions(perms []ec2types.IpPermission, direction string) []graph.SGRule {
	var rules []graph.SGRule
	for _, p := range perms {
		var cidrs []string
		description := ""
		for _, r := range p.IpRanges {
			if r.CidrIp != nil {
				cidrs = append(cidrs, *r.CidrIp)
			}
			if description == "" && r.Description != nil {
				description = *r.Description
			}
		}
		for _, r := range p.Ipv6Ranges {
			if r.CidrIpv6 != nil {
				cidrs = append(cidrs, *r.CidrIpv6)
			}
		}
		var refs []string
		for _, u := range p.UserIdGroupPairs {
			if u.GroupId != nil {
				refs = append(refs, *u.GroupId)
			}
		}
		protocol := aws.ToString(p.IpProtocol)
		if protocol == "" {
			protocol = "-1"
		}
		rules = append(rules, graph.SGRule{
			Direction:       direction,
			Protocol:        protocol,
			FromPort:        p.FromPort,
			ToPort:          p.ToPort,
			CIDRRanges:      cidrs,
			ReferencedSGIDs: refs,
			Description:     description,
		})
	}
	return rules
}

func int32Val(p *int32) int {
	if p == nil {
		return 0
	}
	return int(*p)
}
