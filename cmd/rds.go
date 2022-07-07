package cmd

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

func foo() {
	// Load the Shared AWS Configuration (~/.aws/config)
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatal(err)
	}

	// Create an Amazon S3 service client
	client := rds.NewFromConfig(cfg)

	output, err := client.DescribeDBClusterSnapshots(context.TODO(), &rds.DescribeDBClusterSnapshotsInput{
		DBClusterIdentifier: aws.String(clusterID),
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("results:")
	for _, v := range output.DBClusterSnapshots {
		log.Printf("%+v, %+v\n", aws.ToString(v.DBClusterSnapshotIdentifier), v.SnapshotCreateTime.String())
	}
}
