package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

type createResult struct {
	Cluster  types.DBCluster
	Instance types.DBInstance
}

func rdsClient() (*rds.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return rds.NewFromConfig(cfg), nil
}

func getClusterSnapshot(clusterID string) (types.DBClusterSnapshot, error) {
	var s types.DBClusterSnapshot

	client, err := rdsClient()
	if err != nil {
		return s, err
	}

	output, err := client.DescribeDBClusterSnapshots(context.TODO(), &rds.DescribeDBClusterSnapshotsInput{
		DBClusterIdentifier: aws.String(clusterID),
		MaxRecords:          aws.Int32(20),
		// SnapshotType:        aws.String("automated"),
	})
	if err != nil {
		return s, err
	}

	if len(output.DBClusterSnapshots) == 0 {
		return s, errors.New("no snapshots found")
	}

	sort.Slice(output.DBClusterSnapshots, func(i, j int) bool {
		it := *output.DBClusterSnapshots[i].SnapshotCreateTime
		jt := *output.DBClusterSnapshots[j].SnapshotCreateTime
		return it.Before(jt)
	})

	return output.DBClusterSnapshots[len(output.DBClusterSnapshots)-1], nil
}

func getSnapshot(instanceID string) (types.DBSnapshot, error) {
	var s types.DBSnapshot

	client, err := rdsClient()
	if err != nil {
		return s, err
	}

	output, err := client.DescribeDBSnapshots(context.TODO(), &rds.DescribeDBSnapshotsInput{
		DBInstanceIdentifier: aws.String(instanceID),
		MaxRecords:           aws.Int32(20),
		// SnapshotType:         aws.String("automated"),
	})
	if err != nil {
		return s, err
	}

	if len(output.DBSnapshots) == 0 {
		return s, errors.New("no snapshots found")
	}

	sort.Slice(output.DBSnapshots, func(i, j int) bool {
		it := *output.DBSnapshots[i].SnapshotCreateTime
		jt := *output.DBSnapshots[j].SnapshotCreateTime
		return it.Before(jt)
	})

	return output.DBSnapshots[len(output.DBSnapshots)-1], nil
}

func createDBInstance(snapshot types.DBSnapshot) (createResult, error) {
	var r createResult

	client, err := rdsClient()
	if err != nil {
		return r, err
	}

	instanceID := aws.ToString(snapshot.DBInstanceIdentifier) + "-" + randomString(8)

	iout, err := client.RestoreDBInstanceFromDBSnapshot(context.TODO(), &rds.RestoreDBInstanceFromDBSnapshotInput{
		AutoMinorVersionUpgrade: aws.Bool(false),
		DBInstanceClass:         aws.String("db.t4g.micro"),
		DBInstanceIdentifier:    aws.String(instanceID),
		DBSnapshotIdentifier:    snapshot.DBSnapshotArn,
		Engine:                  snapshot.Engine,
		Iops:                    aws.Int32(0),
		MultiAZ:                 aws.Bool(false),
		PubliclyAccessible:      aws.Bool(false),
	})
	if err != nil {
		return r, err
	}
	r.Instance = *iout.DBInstance

	return r, nil
}

func createDBCluster(snapshot types.DBClusterSnapshot) (createResult, error) {
	var r createResult

	client, err := rdsClient()
	if err != nil {
		return r, err
	}

	clusterID := aws.ToString(snapshot.DBClusterIdentifier) + "-" + randomString(8)

	cout, err := client.RestoreDBClusterFromSnapshot(context.TODO(), &rds.RestoreDBClusterFromSnapshotInput{
		DBClusterIdentifier:    aws.String(clusterID),
		DBClusterInstanceClass: aws.String("db.t4g.micro"),
		Engine:                 snapshot.Engine,
		PubliclyAccessible:     aws.Bool(false),
		SnapshotIdentifier:     snapshot.DBClusterSnapshotArn,
	})
	if err != nil {
		return r, err
	}

	for {
		time.Sleep(5 * time.Second)
		output, err := client.DescribeDBClusters(context.TODO(), &rds.DescribeDBClustersInput{
			DBClusterIdentifier: aws.String(clusterID),
		})
		if err != nil {
			return r, err
		}
		if len(output.DBClusters) > 0 {
			if aws.ToString(output.DBClusters[0].Status) == "available" {
				break
			}
			fmt.Printf("waiting on cluster (%s)\n", aws.ToString(output.DBClusters[0].Status))
		}
	}

	iout, err := client.CreateDBInstance(context.TODO(), &rds.CreateDBInstanceInput{
		AutoMinorVersionUpgrade: aws.Bool(false),
		DBClusterIdentifier:     aws.String(clusterID),
		DBInstanceClass:         aws.String("db.t3.medium"),
		DBInstanceIdentifier:    aws.String(clusterID + "instance-1"),
		Engine:                  snapshot.Engine,
		Iops:                    aws.Int32(0),
		MultiAZ:                 aws.Bool(false),
		PubliclyAccessible:      aws.Bool(false),
	})
	if err != nil {
		return r, err
	}
	r.Cluster = *cout.DBCluster
	r.Instance = *iout.DBInstance

	return r, nil
}
