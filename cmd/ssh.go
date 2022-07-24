package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// func runScripts(dir string, vars []envVar) error {
// 	fmt.Printf("Executing scripts in %s...\n", dir)

// 	scripts, err := getScripts(dir)
// 	if err != nil {
// 		return err
// 	}

// 	for k, v := range scripts {
// 		fmt.Printf("[%d/%d] Calling %s\n", k+1, len(scripts), v.Name())
// 		cmd := exec.Command(dir + "/" + v.Name())
// 		cmd.Stdout = os.Stdout
// 		cmd.Stderr = os.Stderr
// 		if vars != nil {
// 			for _, vv := range vars {
// 				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", vv.Key, vv.Value))
// 			}
// 		}
// 		err := cmd.Run()
// 		if err != nil {
// 			return err
// 		}
// 	}

// 	return nil
// }

func setupSSHTunnel(proxy, targetHost, privateKey string, localPort, remotePort int) error {
	// debug
	fmt.Println(proxy)
	fmt.Println(privateKey)

	tmpFile, err := ioutil.TempFile(os.TempDir(), "rdsvalidator-")
	if err != nil {
		return err
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	_, err = tmpFile.Write([]byte(privateKey))
	if err != nil {
		return err
	}

	// TODO: make port configurable
	fmt.Printf("Waiting on proxy %s:22...", proxy)
	cmd := exec.Command("ssh", "-i", tmpFile.Name(), fmt.Sprintf("ubuntu@%s", proxy), "uname")
	cmd.Stdout = nil
	cmd.Stderr = nil
	for {
		err = cmd.Run()
		if err != nil {
			time.Sleep(1 * time.Second)
			fmt.Print(".")
			continue
		}
		fmt.Println("done.")
		break
	}

	fmt.Print("Setting up tunnel...")
	proxyString := strconv.Itoa(localPort) + fmt.Sprintf(":%s:", targetHost) + strconv.Itoa(remotePort)

	// debug
	fmt.Println(proxyString)

	// TODO: make username configurable
	cmd = exec.Command("ssh", "-i", tmpFile.Name(), "-L", proxyString, fmt.Sprintf("ubuntu@%s", proxy))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}
	fmt.Println("done.")

	// debug
	fmt.Scanln()

	return nil
}
