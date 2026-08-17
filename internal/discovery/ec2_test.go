package discovery

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

type mockEC2 struct {
	instances []ec2types.Instance
	volumes   []ec2types.Volume
}

func (m *mockEC2) DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: m.instances}}}, nil
}
func (m *mockEC2) DescribeVolumes(ctx context.Context, in *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	return &ec2.DescribeVolumesOutput{Volumes: m.volumes}, nil
}

func TestEC2Discoverer(t *testing.T) {
	m := &mockEC2{
		instances: []ec2types.Instance{{
			InstanceId:       aws.String("i-abc"),
			InstanceType:     ec2types.InstanceTypeT3Small,
			State:            &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
			PrivateIpAddress: aws.String("10.0.0.5"),
			VpcId:            aws.String("vpc-1"),
			SubnetId:         aws.String("subnet-1"),
			SecurityGroups:   []ec2types.GroupIdentifier{{GroupId: aws.String("sg-web")}},
			Tags:             []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String("web")}},
			BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{{
				Ebs: &ec2types.EbsInstanceBlockDevice{VolumeId: aws.String("vol-1")},
			}},
		}, {
			InstanceId: aws.String("i-dead"),
			State:      &ec2types.InstanceState{Name: ec2types.InstanceStateNameTerminated},
		}},
		volumes: []ec2types.Volume{{VolumeId: aws.String("vol-1"), Encrypted: aws.Bool(true)}},
	}
	d := NewEC2Discoverer(m, Options{Region: "us-east-1"})
	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)
	assert.Equal(t, "web", res.Resources[0].Label)
	assert.Equal(t, graph.HealthHealthy, res.Resources[0].Health)
	assert.True(t, res.Resources[0].AttrBool("ebs_encrypted"))
	require.Len(t, res.Edges, 1)
	assert.Equal(t, graph.RelationGuardedBy, res.Edges[0].Relation)
}

type mockSG struct {
	sgs  []ec2types.SecurityGroup
	enis []ec2types.NetworkInterface
}

func (m *mockSG) DescribeSecurityGroups(ctx context.Context, in *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: m.sgs}, nil
}
func (m *mockSG) DescribeNetworkInterfaces(ctx context.Context, in *ec2.DescribeNetworkInterfacesInput, _ ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error) {
	return &ec2.DescribeNetworkInterfacesOutput{NetworkInterfaces: m.enis}, nil
}

func TestSecurityGroupDiscoverer(t *testing.T) {
	m := &mockSG{
		sgs: []ec2types.SecurityGroup{{
			GroupId:   aws.String("sg-web"),
			GroupName: aws.String("web"),
			VpcId:     aws.String("vpc-1"),
			IpPermissions: []ec2types.IpPermission{{
				IpProtocol: aws.String("tcp"), FromPort: aws.Int32(80), ToPort: aws.Int32(80),
				UserIdGroupPairs: []ec2types.UserIdGroupPair{{GroupId: aws.String("sg-alb")}},
			}},
		}, {
			GroupId: aws.String("sg-def"), GroupName: aws.String("default"),
		}},
		enis: []ec2types.NetworkInterface{{
			NetworkInterfaceId: aws.String("eni-1"),
			Groups:             []ec2types.GroupIdentifier{{GroupId: aws.String("sg-web")}},
		}},
	}
	d := NewSecurityGroupDiscoverer(m, Options{Region: "us-east-1"})
	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)
	assert.Equal(t, "sg-web", res.Resources[0].ID)
	require.Len(t, res.Edges, 1)
	assert.Equal(t, graph.RelationAllowsIngress, res.Edges[0].Relation)
	assert.Equal(t, "sg-alb", res.Edges[0].From)
}
