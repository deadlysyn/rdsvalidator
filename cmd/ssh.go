package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func setupSSHTunnel(proxy, targetHost, privateKey string, localPort, remotePort int) (*exec.Cmd, error) {
	// debug
	fmt.Println(proxy)
	fmt.Println(privateKey)

	c := &exec.Cmd{}

	tmpFile, err := ioutil.TempFile(os.TempDir(), "rdsvalidator-")
	if err != nil {
		return c, err
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	_, err = tmpFile.Write([]byte(privateKey))
	if err != nil {
		return c, err
	}

	// TODO: make port configurable
	fmt.Printf("Waiting on proxy %s:22...", proxy)
	c = exec.Command("ssh", "-i", tmpFile.Name(), fmt.Sprintf("ubuntu@%s", proxy), "uname")
	c.Stdout = nil
	c.Stderr = nil
	for {
		err = c.Run()
		// debug - some times hangs in this loop
		fmt.Println(err)

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
	c = exec.Command("ssh", "-i", tmpFile.Name(), "-L", proxyString,
		"-fN", "-o 'ExitOnForwardFailure yes'", fmt.Sprintf("ubuntu@%s", proxy))
	err = c.Start()
	if err != nil {
		return c, err
	}
	fmt.Println("done.")

	return c, nil
}
