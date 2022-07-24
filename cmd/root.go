package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	clusterID   string
	instanceID  string
	postDir     string
	preDir      string
	proxy       string
	proxyKey    string
	proxySubnet string
	proxyVPC    string

	instanceType = "db.t3.medium"
	list         = false
	proxyCreate  = false

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
	rootCmd.PersistentFlags().StringVar(&clusterID, "cluster-id", clusterID, "use latest snapshot for specified cluster ID")
	rootCmd.PersistentFlags().StringVar(&instanceID, "instance-id", instanceID, "use latest snapshot for specified instance ID")
	rootCmd.PersistentFlags().StringVar(&instanceType, "instance-type", instanceType, "RDS instance type")
	rootCmd.PersistentFlags().BoolVar(&list, "list", list, "list available DB clusters and instances")
	rootCmd.PersistentFlags().StringVar(&postDir, "post", postDir, "directory containing scripts to execute after DB creation")
	rootCmd.PersistentFlags().StringVar(&preDir, "pre", preDir, "directory containing scripts to execute before DB creation")
	rootCmd.PersistentFlags().StringVar(&proxy, "proxy", proxy, "host used to proxy DB connections")
	rootCmd.PersistentFlags().BoolVar(&proxyCreate, "proxy-create", proxyCreate, "create ephemeral SSH proxy for DB connections")
	rootCmd.PersistentFlags().StringVar(&proxyKey, "proxy-key", proxyKey, "proxy private key")
	rootCmd.PersistentFlags().StringVar(&proxySubnet, "proxy-subnet", proxySubnet, "subnet used to deploy ephemeral SSH proxy")
	rootCmd.PersistentFlags().StringVar(&proxyVPC, "proxy-vpc", proxyVPC, "VPC used to deploy ephemeral SSH proxy")
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

	if len(clusterID) == 0 && len(instanceID) == 0 {
		logger.Fatal(fmt.Errorf("USAGE: Must specify --cluster-id or --instance-id"))
	}

	if len(preDir) > 0 {
		err := runScripts(preDir, nil)
		if err != nil {
			logger.Fatal(err)
		}
	}

	var res createDatabaseResult
	var vars []envVar

	if len(clusterID) > 0 {
		snapshot, err := getClusterSnapshot(ctx, clusterID)
		if err != nil {
			logger.Fatal(err)
		}
		fmt.Printf("Using latest cluster snapshot: '%s' (%s)\n", aws.ToString(snapshot.DBClusterSnapshotIdentifier), snapshot.SnapshotCreateTime.String())

		res, err = createClusterFromSnapshot(ctx, snapshot)
		if err != nil {
			logger.Fatal(err)
		}

		// vars = []envVar{
		// 	{
		// 		Key:   "DB_ENDPOINT",
		// 		Value: aws.ToString(res.Cluster.Endpoint),
		// 	},
		// 	{
		// 		Key:   "DB_NAME",
		// 		Value: aws.ToString(res.Cluster.DatabaseName),
		// 	},
		// 	{
		// 		Key:   "DB_PORT",
		// 		Value: strconv.Itoa(int(aws.ToInt32(res.Cluster.Port))),
		// 	},
		// 	{
		// 		Key:   "DB_USER",
		// 		Value: aws.ToString(res.Cluster.MasterUsername),
		// 	},
		// }
	}

	if len(instanceID) > 0 {
		snapshot, err := getInstanceSnapshot(ctx, instanceID)
		if err != nil {
			logger.Fatal(err)
		}
		fmt.Printf("Using latest instance snapshot: '%s' (%s)\n", aws.ToString(snapshot.DBSnapshotIdentifier), snapshot.SnapshotCreateTime.String())

		res, err = createInstanceFromSnapshot(ctx, snapshot)
		if err != nil {
			logger.Fatal(err)
		}

		// vars = []envVar{
		// 	{
		// 		Key:   "DB_ENDPOINT",
		// 		Value: aws.ToString(res.Instance.Endpoint.Address),
		// 	},
		// 	{
		// 		Key:   "DB_NAME",
		// 		Value: aws.ToString(res.Instance.DBName),
		// 	},
		// 	{
		// 		Key:   "DB_PORT",
		// 		Value: strconv.Itoa(int(res.Instance.Endpoint.Port)),
		// 	},
		// 	{
		// 		Key:   "DB_USER",
		// 		Value: aws.ToString(res.Instance.MasterUsername),
		// 	},
		// }
	}

	vars = []envVar{
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
	// debug
	fmt.Printf("%+v\n", vars)

	// TODO: allow customizing local port
	if len(proxy) > 0 {
		if len(proxyKey) == 0 {
			logger.Fatal(fmt.Errorf("USAGE: Must provide --proxy-key"))
		}

		key, err := ioutil.ReadFile(proxyKey)
		if err != nil {
			logger.Fatal(err)
		}

		err = setupSSHTunnel(proxy,
			aws.ToString(res.Instance.Endpoint.Address),
			string(key),
			int(res.Instance.Endpoint.Port),
			int(res.Instance.Endpoint.Port))
		if err != nil {
			logger.Fatal(err)
		}
	} else if proxyCreate {
		if len(proxyVPC) == 0 || len(proxySubnet) == 0 {
			logger.Fatal(fmt.Errorf("USAGE: Must provide --proxy-vpc and --proxy-subnet"))
			return
		}

		proxy, err := createProxy(ctx)
		if err != nil {
			logger.Fatal(err)
		}
		defer func() {
			deleteInstance(ctx, aws.ToString(proxy.Instance.InstanceId))
			deleteKeypair(ctx, aws.ToString(proxy.Keypair.KeyPairId))
			deleteSecurityGroup(ctx, aws.ToString(proxy.Group.GroupId))
		}()

		err = setupSSHTunnel(aws.ToString(proxy.Instance.PublicIpAddress),
			aws.ToString(res.Instance.Endpoint.Address),
			aws.ToString(proxy.Keypair.KeyMaterial),
			int(res.Instance.Endpoint.Port),
			int(res.Instance.Endpoint.Port))
		if err != nil {
			logger.Fatal(err)
		}
	} else {
		logger.Fatal(fmt.Errorf("USAGE: Must provide --proxy or --proxy-create"))
		return
	}

	if len(postDir) > 0 {
		err := runScripts(postDir, vars)
		if err != nil {
			logger.Fatal(err)
		}
	}

	// catch signals / cleanup

	cmd.Help()
}
