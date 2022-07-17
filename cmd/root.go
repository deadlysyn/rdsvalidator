package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile       string
	clusterID     string
	listDatabases = false
	listSnapshots = false

	logger *log.Logger
)

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
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().StringVar(&clusterID, "cluster-id", clusterID, "pattern used to match snapshot name")
	rootCmd.PersistentFlags().BoolVar(&listDatabases, "list-db", listDatabases, "list available DB clusters and instances")
	rootCmd.PersistentFlags().BoolVar(&listSnapshots, "list-snapshot", listSnapshots, "list available DB snapshots")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(cfgFile)
	}

	viper.SetConfigName("config") // name of config file (without extension)
	viper.AddConfigPath(".")      // adding home directory as first search path
	viper.SetEnvPrefix("RV")
	viper.AutomaticEnv() // read in environment variables that match RV_*

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func main(cmd *cobra.Command, args []string) {
	if listDatabases {
		res, err := getDatabases()
		if err != nil {
			logger.Fatal(err)
		}
		printDatabases(res)
		return
	}

	// snapshot, err := getSnapshot(clusterID)
	// if err != nil {
	// 	logger.Fatal(err)
	// }
	// fmt.Printf("%+v %+v\n", aws.ToString(snapshot.DBSnapshotIdentifier), snapshot.SnapshotCreateTime)

	// snapshot, err := getClusterSnapshot(clusterID)
	// if err != nil {
	// 	logger.Fatal(err)
	// }
	// fmt.Printf("%+v %+v\n", aws.ToString(snapshot.DBClusterSnapshotIdentifier), snapshot.SnapshotCreateTime)

	// res, err := createDBCluster(snapshot)
	// if err != nil {
	// 	logger.Fatal(err)
	// }
	// fmt.Printf("%+v\n", aws.ToString(res.Cluster.Endpoint))
}
