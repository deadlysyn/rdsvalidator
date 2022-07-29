package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strconv"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
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

type bagOfHolding []interface{}

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

func catchSignal(b *bagOfHolding) {
	sig := make(chan os.Signal, 1)
	// signal.Notify(sig)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGQUIT)
	_ = <-sig
	cleanup(b)
}

func cleanup(b *bagOfHolding) {
	fmt.Println("Starting cleanup...")
	// walk through created resources in reverse
	for i := len(*b) - 1; i >= 0; i-- {
		v := (*b)[i]
		switch v.(type) {
		case *exec.Cmd:
			defer v.(exec.Cmd).Process.Release()
		case *ec2.CreateKeyPairOutput:
			k := aws.ToString(v.(ec2.CreateKeyPairOutput).KeyPairId)
			deleteKeypair(context.TODO(), k)
		case types.Instance:
			i := aws.ToString(v.(types.Instance).InstanceId)
			deleteProxy(context.TODO(), i)
		case createDBResult:
			d := aws.ToString(v.(createDBResult).Instance.DBInstanceIdentifier)
			deleteDatabaseInstance(context.TODO(), d)
		case *ec2.CreateSecurityGroupOutput:
			g := aws.ToString(v.(ec2.CreateSecurityGroupOutput).GroupId)
			deleteSecurityGroup(context.TODO(), g)
		default:
			fmt.Println(reflect.TypeOf(v).String())
		}
	}

	os.Exit(255)
}

func main(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	var state bagOfHolding // copy of created resources
	go catchSignal(&state) // cleanup on signal
	defer cleanup(&state)  // cleanup on normal return

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

	if (len(clusterID) == 0 && len(instanceID) == 0) || (len(clusterID) > 0 && len(instanceID) > 0) {
		logger.Fatal("USAGE: Must specify one of --cluster-id or --instance-id")
	}

	if len(preDir) > 0 {
		err := runScripts(preDir, nil)
		if err != nil {
			logger.Fatal(err)
		}
	}

	// create security group now so we have id when creating database
	var groupID string
	if proxyCreate {
		if len(proxyVPC) == 0 || len(proxySubnet) == 0 {
			logger.Fatal("USAGE: Must provide --proxy-vpc and --proxy-subnet")
		}

		sg, err := createSecurityGroup(ctx, proxyVPC)
		if err != nil {
			logger.Fatal(err)
		}

		groupID = aws.ToString(sg.GroupId)
		state = append(state, sg)
	}

	var res createDBResult
	if len(clusterID) > 0 {
		snapshot, err := getClusterSnapshot(ctx, clusterID)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}
		fmt.Printf("Using latest cluster snapshot: '%s' (%s)\n", aws.ToString(snapshot.DBClusterSnapshotIdentifier), snapshot.SnapshotCreateTime.String())

		res, err = createClusterFromSnapshot(ctx, snapshot, groupID)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}
	}

	if len(instanceID) > 0 {
		snapshot, err := getInstanceSnapshot(ctx, instanceID)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}
		fmt.Printf("Using latest instance snapshot: '%s' (%s)\n", aws.ToString(snapshot.DBSnapshotIdentifier), snapshot.SnapshotCreateTime.String())

		res, err = createInstanceFromSnapshot(ctx, snapshot, groupID)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}
	}

	state = append(state, res)

	// TODO: allow customizing ports
	dbHost := aws.ToString(res.Instance.Endpoint.Address)
	dbPort := int(res.Instance.Endpoint.Port)
	localPort := dbPort + 10000

	if proxyCreate || len(proxy) > 0 {
		dbHost = "localhost"
	}

	vars := []envVar{
		{
			Key:   "DB_HOST",
			Value: dbHost,
		},
		{
			Key:   "DB_NAME",
			Value: res.Instance.DBName,
		},
		{
			Key:   "DB_PORT",
			Value: strconv.Itoa(localPort),
		},
		{
			Key:   "DB_USER",
			Value: aws.ToString(res.Instance.MasterUsername),
		},
	}

	var c *exec.Cmd
	if len(proxy) > 0 {
		if len(proxyKey) == 0 {
			logger.Println("USAGE: Must provide --proxy-key")
			return // make sure defer runs
		}

		key, err := ioutil.ReadFile(proxyKey)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}

		c, err = setupSSHTunnel(proxy, aws.ToString(res.Instance.Endpoint.Address), string(key), localPort, dbPort)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}
	} else if proxyCreate {
		proxy, err := createProxy(ctx, groupID)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}

		proxyAddr := aws.ToString(proxy.Instance.PublicIpAddress)
		key := aws.ToString(proxy.Keypair.KeyMaterial)
		c, err = setupSSHTunnel(proxyAddr, aws.ToString(res.Instance.Endpoint.Address), key, localPort, dbPort)
		if err != nil {
			logger.Println(err)
			return // make sure defer runs
		}
		state = append(state, proxy.Instance, proxy.Keypair)
	}
	state = append(state, c)

	if len(postDir) > 0 {
		err := runScripts(postDir, vars)
		if err != nil {
			logger.Fatal(err)
		}
	}

	// debug
	fmt.Println("Waiting...")
	fmt.Scanln()

	cmd.Help()
	return // make sure defer runs
}
