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

func runScripts(dir string, vars envVars) error {
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
		for _, vv := range vars {
			cmd.Env = append(cmd.Env, fmt.Sprint(vv.Key, vv.Value))
		}
		err := cmd.Run()
		if err != nil {
			return err
		}
	}

	return nil
}
