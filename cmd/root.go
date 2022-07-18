package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	clusterID  string
	instanceID string
	preDir     string
	postDir    string
	list       = false

	logger *log.Logger
)

type envVar struct {
	Key   string
	Value interface{}
}

type envVars []envVar

var rootCmd = &cobra.Command{
	Use:   "rdsvalidator",
	Short: "CLI to automate validation of RDS backups",
	Run:   main,
}

// Execute adds all child commands to the root command and sets flags
// appropriately. This is called by main.main(). It only needs to happen
// once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	logger = log.New(os.Stderr, "", log.Lshortfile)

	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&clusterID, "cluster-id", clusterID, "use latest snapshot for specified cluster ID")
	rootCmd.PersistentFlags().StringVar(&instanceID, "instance-id", instanceID, "use latest snapshot for specified instance ID")
	rootCmd.PersistentFlags().BoolVar(&list, "list", list, "list available DB clusters and instances")
	rootCmd.PersistentFlags().StringVar(&preDir, "pre", preDir, "directory containing scripts to execute before DB creation")
	rootCmd.PersistentFlags().StringVar(&postDir, "post", postDir, "directory containing scripts to execute after DB creation")
}

func initConfig() {
	viper.SetEnvPrefix("RV")
	viper.AutomaticEnv() // read in environment variables that match RV_*
}

func main(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	if list {
		res, err := getDatabases(ctx)
		if err != nil {
			logger.Fatal(err)
		}
		err = printDatabases(res)
		if err != nil {
			logger.Fatal(err)
		}
		return
	}

	if len(clusterID) > 0 && len(instanceID) > 0 {
		logger.Fatal(fmt.Errorf("USAGE: Must specify one of --cluster-id or --instance-id"))
	}

	if len(clusterID) > 0 {
		err := runScripts(preDir, "")
		if err != nil {
			logger.Fatal(err)
		}

		s, err := getClusterSnapshot(ctx, clusterID)
		if err != nil {
			logger.Fatal(err)
		}
		fmt.Printf("Using latest cluster snapshot: %s (%s)\n", aws.ToString(s.DBClusterSnapshotIdentifier), s.SnapshotCreateTime.String())

		res, err := createClusterFromSnapshot(ctx, s)
		if err != nil {
			logger.Fatal(err)
		}

		v := envVars{
			envVar{
				Key:   "DB_ENDPOINT",
				Value: aws.ToString(res.Cluster.Endpoint),
			},
			envVar{
				Key:   "DB_NAME",
				Value: aws.ToString(res.Cluster.DatabaseName),
			},
			envVar{
				Key:   "DB_PORT",
				Value: aws.ToInt32(res.Cluster.Port),
			},
			envVar{
				Key:   "DB_USER",
				Value: aws.ToString(res.Cluster.MasterUsername),
			},
		}

		err = runScripts(postDir, v)
		if err != nil {
			logger.Fatal(err)
		}

		return
	}

	if len(instanceID) > 0 {
		err := runScripts(preDir, "")
		if err != nil {
			logger.Fatal(err)
		}

		s, err := getInstanceSnapshot(ctx, clusterID)
		if err != nil {
			logger.Fatal(err)
		}
		fmt.Printf("Using latest instance snapshot: %s (%s)\n", aws.ToString(s.DBSnapshotIdentifier), s.SnapshotCreateTime.String())

		res, err := createInstanceFromSnapshot(ctx, s)
		if err != nil {
			logger.Fatal(err)
		}

		v := envVars{
			envVar{
				Key:   "DB_ENDPOINT",
				Value: aws.ToString(res.Instance.Endpoint.Address),
			},
			envVar{
				Key:   "DB_NAME",
				Value: aws.ToString(res.Instance.DBName),
			},
			envVar{
				Key:   "DB_PORT",
				Value: aws.ToInt32(&res.Instance.Endpoint.Port),
			},
			envVar{
				Key:   "DB_USER",
				Value: aws.ToString(res.Instance.MasterUsername),
			},
		}
		err = runScripts(postDir, v)
		if err != nil {
			logger.Fatal(err)
		}

		return
	}

	cmd.Help()
}
