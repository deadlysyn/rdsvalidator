package cmd

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
)

func getScripts(dir string) ([]fs.FileInfo, error) {
	scripts, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	return scripts, nil
}

func runScripts(dir, endpoint string) error {
	fmt.Printf("Executing scripts in %s...\n", dir)

	scripts, err := getScripts(dir)
	if err != nil {
		return err
	}

	for k, v := range scripts {
		fmt.Printf("[%d/%d] Calling %s\n", k+1, len(scripts), v.Name())
		cmd := exec.Command(dir + "/" + v.Name())
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if endpoint != "" {
			cmd.Env = append(cmd.Env, fmt.Sprint("ENDPOINT=", endpoint))
		}
		err := cmd.Run()
		if err != nil {
			return err
		}
	}

	return nil
}
