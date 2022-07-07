package cmd

import (
	"context"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

func rdsClient() (*rds.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return rds.NewFromConfig(cfg), nil
}

func getSnapshot(clusterID string) (types.DBClusterSnapshot, error) {
	client, err := rdsClient()
	if err != nil {
		return types.DBClusterSnapshot{}, err
	}

	output, err := client.DescribeDBClusterSnapshots(context.TODO(), &rds.DescribeDBClusterSnapshotsInput{
		DBClusterIdentifier: aws.String(clusterID),
		MaxRecords:          aws.Int32(20),
		// SnapshotType:        aws.String("automated"),
	})
	if err != nil {
		return types.DBClusterSnapshot{}, err
	}

	sort.Slice(output.DBClusterSnapshots, func(i, j int) bool {
		it := *output.DBClusterSnapshots[i].SnapshotCreateTime
		jt := *output.DBClusterSnapshots[j].SnapshotCreateTime
		return it.Before(jt)
	})

	return output.DBClusterSnapshots[len(output.DBClusterSnapshots)-1], nil
}
