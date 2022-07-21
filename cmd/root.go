package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	bastionVPC    string
	bastionSubnet string
	clusterID     string
	createBastion = false
	instanceID    string
	instanceType  = "db.t3.medium"
	preDir        string
	postDir       string
	list          = false

	logger *log.Logger
)

type envVar struct {
	Key   string
	Value interface{}
}

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
	rootCmd.PersistentFlags().StringVar(&bastionVPC, "bastion-vpc", bastionVPC, "VPC used to deploy ephemeral bastion")
	rootCmd.PersistentFlags().StringVar(&bastionSubnet, "bastion-subnet", bastionSubnet, "subnet used to deploy ephemeral bastion")
	rootCmd.PersistentFlags().BoolVar(&createBastion, "create-bastion", createBastion, "create bastion host used to proxy DB connections")
	rootCmd.PersistentFlags().StringVar(&clusterID, "cluster-id", clusterID, "use latest snapshot for specified cluster ID")
	rootCmd.PersistentFlags().StringVar(&instanceID, "instance-id", instanceID, "use latest snapshot for specified instance ID")
	rootCmd.PersistentFlags().StringVar(&instanceType, "instance-type", instanceType, "RDS instance type")
	rootCmd.PersistentFlags().BoolVar(&list, "list", list, "list available DB clusters and instances")
	rootCmd.PersistentFlags().StringVar(&preDir, "pre", preDir, "directory containing scripts to execute before DB creation")
	rootCmd.PersistentFlags().StringVar(&postDir, "post", postDir, "directory containing scripts to execute after DB creation")
}

func initConfig() {
	viper.SetEnvPrefix("RV")
	viper.AutomaticEnv() // read in environment variables that match RV_*
}

// TODO: refactor as smaller functions
func main(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// testing
	if createBastion {
		if len(bastionVPC) == 0 || len(bastionSubnet) == 0 {
			cmd.Help()
			return
		}
		// this needs to come later and pass a specific cluster or instance
		res, err := getDatabases(ctx)
		if err != nil {
			logger.Fatal(err)
		}

		err = createInstance(ctx, res)
		if err != nil {
			logger.Fatal(err)
		}
		return
	}

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
		err := runScripts(preDir, nil)
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

		v := []envVar{
			{
				Key:   "DB_ENDPOINT",
				Value: aws.ToString(res.Cluster.Endpoint),
			},
			{
				Key:   "DB_NAME",
				Value: aws.ToString(res.Cluster.DatabaseName),
			},
			{
				Key:   "DB_PORT",
				Value: strconv.Itoa(int(aws.ToInt32(res.Cluster.Port))),
			},
			{
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
		err := runScripts(preDir, nil)
		if err != nil {
			logger.Fatal(err)
		}

		s, err := getInstanceSnapshot(ctx, instanceID)
		if err != nil {
			logger.Fatal(err)
		}
		fmt.Printf("Using latest instance snapshot: %s (%s)\n", aws.ToString(s.DBSnapshotIdentifier), s.SnapshotCreateTime.String())

		res, err := createInstanceFromSnapshot(ctx, s)
		if err != nil {
			logger.Fatal(err)
		}

		v := []envVar{
			{
				Key:   "DB_ENDPOINT",
				Value: aws.ToString(res.Instance.Endpoint.Address),
			},
			{
				Key:   "DB_NAME",
				Value: aws.ToString(res.Instance.DBName),
			},
			{
				Key:   "DB_PORT",
				Value: strconv.Itoa(int(res.Instance.Endpoint.Port)),
			},
			{
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
