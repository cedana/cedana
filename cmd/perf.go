package cmd

// This file contains all the benchmarking-related commands when starting `cedana perf ...`

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var perfCmd = &cobra.Command{
	Use:   "perf",
	Short: "Analyze Cedana performance",
}

var perfCritCmd = &cobra.Command{
	Use:   "crit",
	Short: "CRiu Image Tool",
}

var perfCritShowCmd = &cobra.Command{
	Use:   "show /checkpointPath",
	Short: "convert criu image from binary to human-readable json",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires a checkpoint path (directory or image) argument, use cedana ps to see checkpoint locations")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ckptPath := args[0]
		fi, err := os.Stat(ckptPath)
		if err != nil {
			return err
		}
		show_file := func(path string) error {
			cmd := exec.Command("crit", "show", path)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				return fmt.Errorf(out.String())
			} else {
				ext := filepath.Ext(path)
				jsonPath := path[:len(path)-len(ext)] + ".json"
				file, err := os.Create(jsonPath)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = out.WriteTo(file)
				if err != nil {
					return err
				}
				fmt.Printf("converted criu image %s to %s\n", path, jsonPath)
				return nil
			}
		}
		if fi.IsDir() == true { // `crit show` all possible files in dir
			files, err := os.ReadDir(ckptPath)
			if err != nil {
				return err
			}
			for _, file := range files {
				is_dump_log := (file.Name() == "cedana-dump.log" && file.Name() != "dump.log")
				is_json_file := strings.HasSuffix(file.Name(), ".json")
				is_pages_file := strings.HasPrefix(file.Name(), "pages-")
				is_core_file := strings.HasPrefix(file.Name(), "core-")
				if !is_dump_log && !is_json_file && !is_pages_file && !is_core_file {
					err := show_file(filepath.Join(ckptPath, file.Name()))
					if err != nil {
						return err
					}
				}
			}
		} else { // `crit show` single file
			return show_file(ckptPath)
		}
		return nil
	},
}

func init() {
	perfCritCmd.AddCommand(perfCritShowCmd)
	perfCmd.AddCommand(perfCritCmd)

	rootCmd.AddCommand(perfCmd)
}
