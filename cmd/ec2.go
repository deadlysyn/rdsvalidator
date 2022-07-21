package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

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

	_, err = client.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
		KeyPairId: aws.String(keypairID),
	})
	if err != nil {
		return err
	}

	return nil
}

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

	_, err = client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(groupID),
	})
	if err != nil {
		return err
	}

	return nil
}

func createInstance(ctx context.Context, res getResult) error {
	client, err := ec2Client(ctx)
	if err != nil {
		return err
	}

	k, err := createKeypair(ctx)
	if err != nil {
		return err
	}

	g, err := createSecurityGroup(ctx, bastionVPC)
	if err != nil {
		return err
	}

	// debug
	fmt.Println(aws.ToString(k.KeyMaterial))
	fmt.Println(aws.ToString(g.GroupId))

	i, err := client.RunInstances(ctx, &ec2.RunInstancesInput{
		MaxCount:     aws.Int32(1),
		MinCount:     aws.Int32(1),
		ImageId:      aws.String("ami-0b2a3228cbf805ced"), // search to get latest or customize
		InstanceType: types.InstanceTypeT4gNano,           // or types.InstanceTypeT3Nano
		KeyName:      k.KeyName,
		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{
			{
				AssociatePublicIpAddress: aws.Bool(true),
				DeleteOnTermination:      aws.Bool(true),
				DeviceIndex:              aws.Int32(0),
				Groups:                   []string{*g.GroupId},
				SubnetId:                 aws.String(bastionSubnet), // have to handle cluster vs instance differently
			},
		},
	})
	if err != nil {
		return err
	}

	instanceID := aws.ToString(i.Instances[0].InstanceId)
	fmt.Printf("Creating ec2 instance %s...", instanceID)

	for {
		time.Sleep(1 * time.Second)
		id, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceID},
		})
		if err != nil {
			return err
		}
		if len(id.Reservations) > 0 && id.Reservations[0].Instances[0].PublicIpAddress != nil {
			fmt.Println("done.")
			// debug
			fmt.Println(aws.ToString(id.Reservations[0].Instances[0].InstanceId))
			fmt.Println(aws.ToString(id.Reservations[0].Instances[0].PublicIpAddress))
			break
		}
		fmt.Print(".")
	}

	return nil
}

func deleteInstance(ctx context.Context, instanceID string) error {
	client, err := ec2Client(ctx)
	if err != nil {
		return err
	}

	_, err = client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return err
	}

	return nil
}
