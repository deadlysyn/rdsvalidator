package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfg       string
	clusterID string
)

var rootCmd = &cobra.Command{
	Use:   "rdsvalidator",
	Short: "CLI to automate validation of RDS backups",
	Run:   runner,
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
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfg, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().StringVar(&clusterID, "cluster-id", clusterID, "pattern used to match snapshot name")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfg != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(cfg)
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

func runner(cmd *cobra.Command, args []string) {
	foo()
}
