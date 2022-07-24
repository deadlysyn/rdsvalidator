package cmd

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type ec2Instance struct {
	Instance types.Instance
	Group    *ec2.CreateSecurityGroupOutput
	Keypair  *ec2.CreateKeyPairOutput
}

func ec2Client(ctx context.Context) (*ec2.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	return ec2.NewFromConfig(cfg), nil
}

func createKeypair(ctx context.Context) (*ec2.CreateKeyPairOutput, error) {
	k := &ec2.CreateKeyPairOutput{}

	client, err := ec2Client(ctx)
	if err != nil {
		return k, err
	}

	kpName := "rdsvalidator-" + randomString(8)
	fmt.Printf("Creating keypair %s...", kpName)

	k, err = client.CreateKeyPair(ctx, &ec2.CreateKeyPairInput{
		KeyName: aws.String(kpName),
		KeyType: types.KeyTypeEd25519,
	})
	if err != nil {
		return k, err
	}

	for {
		time.Sleep(1 * time.Second)
		kd, err := client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
			KeyNames: []string{kpName},
		})
		if err != nil {
			return k, err
		}
		if len(kd.KeyPairs) > 0 {
			fmt.Println("done.")
			break
		}
		fmt.Print(".")
	}

	return k, nil
}

func deleteKeypair(ctx context.Context, keypairID string) error {
	client, err := ec2Client(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Deleting keypair %s...", keypairID)
	_, err = client.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
		KeyPairId: aws.String(keypairID),
	})
	if err != nil {
		return err
	}
	fmt.Println("done.")

	return nil
}

// TODO: allow passing list of ingress cidrs
func createSecurityGroup(ctx context.Context, vpcID string) (*ec2.CreateSecurityGroupOutput, error) {
	g := &ec2.CreateSecurityGroupOutput{}

	client, err := ec2Client(ctx)
	if err != nil {
		return g, err
	}

	sgName := "rdsvalidator-" + randomString(8)
	fmt.Printf("Creating security group %s...", sgName)

	g, err = client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(sgName),
		Description: aws.String("grant temporary ssh access for rds validator"),
		VpcId:       aws.String(vpcID),
	})
	if err != nil {
		return g, err
	}

	for {
		time.Sleep(1 * time.Second)
		gd, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			GroupIds: []string{aws.ToString(g.GroupId)},
		})
		if err != nil {
			return g, err
		}
		if gd.SecurityGroups[0].IpPermissionsEgress != nil {
			fmt.Println("done.")
			break
		}
		fmt.Print(".")
	}

	_, err = client.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:    g.GroupId,
		CidrIp:     aws.String("0.0.0.0/0"),
		FromPort:   aws.Int32(22),
		ToPort:     aws.Int32(22),
		IpProtocol: aws.String("tcp"),
	})
	if err != nil {
		return g, err
	}

	return g, nil
}

func deleteSecurityGroup(ctx context.Context, groupID string) error {
	client, err := ec2Client(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Deleting security group %s...", groupID)
	_, err = client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(groupID),
	})
	if err != nil {
		return err
	}
	fmt.Println("done.")

	return nil
}

func createProxy(ctx context.Context) (ec2Instance, error) {
	i := ec2Instance{}

	client, err := ec2Client(ctx)
	if err != nil {
		return i, err
	}

	k, err := createKeypair(ctx)
	if err != nil {
		return i, err
	}
	i.Keypair = k

	g, err := createSecurityGroup(ctx, proxyVPC)
	if err != nil {
		return i, err
	}
	i.Group = g

	// TODO: allow passing ami id
	img, err := client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("name"),
				Values: []string{"ubuntu/images/*ubuntu-focal-20.*-server-*"},
			},
			{
				Name:   aws.String("architecture"),
				Values: []string{"arm64"},
			},
			{
				Name:   aws.String("virtualization-type"),
				Values: []string{"hvm"},
			},
		},
		Owners: []string{"099720109477"},
	})
	if err != nil {
		return i, err
	}

	sort.Slice(img.Images, func(i, j int) bool {
		it, _ := time.Parse(time.RFC3339, aws.ToString(img.Images[i].CreationDate))
		jt, _ := time.Parse(time.RFC3339, aws.ToString(img.Images[j].CreationDate))
		return it.Before(jt)
	})

	iout, err := client.RunInstances(ctx, &ec2.RunInstancesInput{
		MaxCount:     aws.Int32(1),
		MinCount:     aws.Int32(1),
		ImageId:      img.Images[len(img.Images)-1].ImageId,
		InstanceType: types.InstanceTypeT4gNano,
		KeyName:      k.KeyName,
		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{
			{
				AssociatePublicIpAddress: aws.Bool(true),
				DeleteOnTermination:      aws.Bool(true),
				DeviceIndex:              aws.Int32(0),
				Groups:                   []string{*g.GroupId},
				SubnetId:                 aws.String(proxySubnet),
			},
		},
	})
	if err != nil {
		return i, err
	}

	instanceID := aws.ToString(iout.Instances[0].InstanceId)
	fmt.Printf("Creating ec2 instance %s...", instanceID)

	for {
		time.Sleep(1 * time.Second)
		id, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceID},
		})
		if err != nil {
			return i, err
		}
		if len(id.Reservations) > 0 && id.Reservations[0].Instances[0].PublicIpAddress != nil {
			fmt.Println("done.")
			i.Instance = id.Reservations[0].Instances[0]
			break
		}
		fmt.Print(".")
	}

	return i, nil
}

func deleteProxy(ctx context.Context, instanceID string) error {
	client, err := ec2Client(ctx)
	if err != nil {
		return err
	}

	t, err := client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return err
	}

	fmt.Printf("Terminating ec2 instance %s...", instanceID)

	// must be terminated to delete security group
	for t.TerminatingInstances[0].CurrentState.Name != "terminated" {
		time.Sleep(1 * time.Second)
		t, err = client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
			InstanceIds: []string{instanceID},
		})
		if err != nil {
			return err
		}
		fmt.Print(".")
	}
	fmt.Println("done.")

	return nil
}
