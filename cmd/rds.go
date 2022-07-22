package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

type createDBResult struct {
	Cluster  types.DBCluster
	Instance types.DBInstance
}

type getDBResult struct {
	Clusters  []types.DBCluster
	Instances []types.DBInstance
}

type clusterMember struct {
	Identifier string `json:"identifier,omitempty"`
	Writer     bool   `json:"writer,omitempty"`
}

type cluster struct {
	Identifier string          `json:"identifier,omitempty"`
	Status     string          `json:"status,omitempty"`
	Members    []clusterMember `json:"members,omitempty"`
}

type instance struct {
	Identifier string `json:"identifier,omitempty"`
	Status     string `json:"status,omitempty"`
}

type databasesOutput struct {
	Clusters  []cluster  `json:"clusters,omitempty"`
	Instances []instance `json:"instances,omitempty"`
}

func rdsClient(ctx context.Context) (*rds.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	return rds.NewFromConfig(cfg), nil
}

func getDatabases(ctx context.Context) (getDBResult, error) {
	var r getDBResult

	client, err := rdsClient(ctx)
	if err != nil {
		return r, err
	}

	cout, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
		IncludeShared: true,
	})
	if err != nil {
		return r, err
	}
	r.Clusters = cout.DBClusters
	// handle pagination
	for cout.Marker != nil {
		cout, err = client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
			IncludeShared: true,
			Marker:        cout.Marker,
		})
		if err != nil {
			return r, err
		}
		r.Clusters = append(r.Clusters, cout.DBClusters...)
	}

	iout, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{})
	if err != nil {
		return r, err
	}
	r.Instances = iout.DBInstances
	// handle pagination
	for iout.Marker != nil {
		iout, err = client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			Marker: iout.Marker,
		})
		if err != nil {
			return r, err
		}
		r.Instances = append(r.Instances, iout.DBInstances...)
	}

	return r, nil
}

func printDatabases(r getDBResult) error {
	var out databasesOutput

	for _, v := range r.Clusters {
		var c cluster
		c.Identifier = aws.ToString(v.DBClusterIdentifier)
		c.Status = aws.ToString(v.Status)
		for _, vv := range v.DBClusterMembers {
			var m clusterMember
			m.Identifier = aws.ToString(vv.DBInstanceIdentifier)
			m.Writer = vv.IsClusterWriter
			c.Members = append(c.Members, m)
		}
		out.Clusters = append(out.Clusters, c)
	}

	var nonClusterInstances []types.DBInstance
	for _, v := range r.Instances {
		if v.DBClusterIdentifier == nil {
			nonClusterInstances = append(nonClusterInstances, v)
		}
	}
	for _, v := range nonClusterInstances {
		var i instance
		i.Identifier = aws.ToString(v.DBInstanceIdentifier)
		i.Status = aws.ToString(v.DBInstanceStatus)
		out.Instances = append(out.Instances, i)
	}

	j, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", j)

	return nil
}

func getClusterSnapshot(ctx context.Context, clusterID string) (types.DBClusterSnapshot, error) {
	var s types.DBClusterSnapshot

	client, err := rdsClient(ctx)
	if err != nil {
		return s, err
	}

	output, err := client.DescribeDBClusterSnapshots(ctx, &rds.DescribeDBClusterSnapshotsInput{
		DBClusterIdentifier: aws.String(clusterID),
		MaxRecords:          aws.Int32(20),
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

func getInstanceSnapshot(ctx context.Context, instanceID string) (types.DBSnapshot, error) {
	var s types.DBSnapshot

	client, err := rdsClient(ctx)
	if err != nil {
		return s, err
	}

	output, err := client.DescribeDBSnapshots(ctx, &rds.DescribeDBSnapshotsInput{
		DBInstanceIdentifier: aws.String(instanceID),
		MaxRecords:           aws.Int32(20),
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

// https://stackoverflow.com/questions/35709153/disabling-aws-rds-backups-when-creating-updating-instances/35730978#35730978
func createClusterFromSnapshot(ctx context.Context, s types.DBClusterSnapshot) (createDBResult, error) {
	var r createDBResult

	client, err := rdsClient(ctx)
	if err != nil {
		return r, err
	}

	clusterID := aws.ToString(s.DBClusterIdentifier) + "-" + randomString(8)

	cout, err := client.RestoreDBClusterFromSnapshot(ctx, &rds.RestoreDBClusterFromSnapshotInput{
		DBClusterIdentifier:    aws.String(clusterID),
		DBClusterInstanceClass: aws.String(instanceType),
		Engine:                 s.Engine,
		PubliclyAccessible:     aws.Bool(false),
		SnapshotIdentifier:     s.DBClusterSnapshotArn,
	})
	if err != nil {
		return r, err
	}

	fmt.Printf("Waiting on cluster (%s)...", aws.ToString(cout.DBCluster.DBClusterIdentifier))
	for {
		time.Sleep(5 * time.Second)
		output, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
			DBClusterIdentifier: aws.String(clusterID),
		})
		if err != nil {
			return r, err
		}
		if len(output.DBClusters) > 0 {
			if aws.ToString(output.DBClusters[0].Status) == "available" {
				fmt.Println("ready!")
				r.Cluster = output.DBClusters[0]
				break
			}
			fmt.Print(".")
		}
	}

	iout, err := client.CreateDBInstance(ctx, &rds.CreateDBInstanceInput{
		AutoMinorVersionUpgrade: aws.Bool(false),
		BackupRetentionPeriod:   aws.Int32(0),
		DBClusterIdentifier:     aws.String(clusterID),
		DBInstanceClass:         aws.String(instanceType),
		DBInstanceIdentifier:    aws.String(clusterID + "-" + "instance-1"),
		Engine:                  s.Engine,
		Iops:                    aws.Int32(0),
		MultiAZ:                 aws.Bool(false),
		PubliclyAccessible:      aws.Bool(false),
	})
	if err != nil {
		return r, err
	}

	fmt.Printf("Waiting on instance (%s)...", aws.ToString(iout.DBInstance.DBInstanceIdentifier))
	for {
		time.Sleep(5 * time.Second)
		output, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			DBInstanceIdentifier: iout.DBInstance.DBInstanceIdentifier,
		})
		if err != nil {
			return r, err
		}
		if aws.ToString(output.DBInstances[0].DBInstanceStatus) == "available" {
			fmt.Println("ready!")
			r.Instance = output.DBInstances[0]
			break
		}
		fmt.Print(".")
	}

	return r, nil
}

// https://stackoverflow.com/questions/35709153/disabling-aws-rds-backups-when-creating-updating-instances/35730978#35730978
func createInstanceFromSnapshot(ctx context.Context, s types.DBSnapshot) (createDBResult, error) {
	var r createDBResult

	client, err := rdsClient(ctx)
	if err != nil {
		return r, err
	}

	instanceID := aws.ToString(s.DBInstanceIdentifier) + "-" + randomString(8)

	iout, err := client.RestoreDBInstanceFromDBSnapshot(ctx, &rds.RestoreDBInstanceFromDBSnapshotInput{
		AutoMinorVersionUpgrade: aws.Bool(false),
		DBInstanceClass:         aws.String(instanceType),
		DBInstanceIdentifier:    aws.String(instanceID),
		DBSnapshotIdentifier:    s.DBSnapshotArn,
		Engine:                  s.Engine,
		Iops:                    aws.Int32(0),
		MultiAZ:                 aws.Bool(false),
		PubliclyAccessible:      aws.Bool(false),
	})
	if err != nil {
		return r, err
	}

	fmt.Printf("Waiting on instance (%s)...", aws.ToString(iout.DBInstance.DBInstanceIdentifier))
	for {
		time.Sleep(5 * time.Second)
		output, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			DBInstanceIdentifier: aws.String(instanceID),
		})
		if err != nil {
			return r, err
		}
		if aws.ToString(output.DBInstances[0].DBInstanceStatus) == "available" {
			r.Instance = output.DBInstances[0]
			fmt.Println("ready!")
			break
		}
		fmt.Print(".")
	}

	return r, nil
}
