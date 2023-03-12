// Copyright (c) 2019, Sylabs Inc. All rights reserved.
// Copyright (c) 2021, NVIDIA CORPORATION. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package autotools

import (
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BTMichalowicz/go_exec/pkg/advexec"
	"github.com/BTMichalowicz/go_util/pkg/util"
)

// Config represents the configuration of the autotools-compliant software to configure/compile/install
type Config struct {
	// DetectDone specifies whether Detect() has been called on the configuration
	DetectDone bool

	// Install is the path to the directory where the software should be installed
	Install string

	// Source is the path to the directory where the source code is
	Source string

	// ExtraConfigureArgs is a set of string that are passed to configure
	ExtraConfigureArgs []string

	// ConfigureEnv is the environment to use when running configure
	ConfigureEnv []string

	// HasAutogen specifies whether the package has a autogen.sh file
	HasAutogen bool

	// HasConfigure specifies whether the package has a configure file (true also if HadAutogen is true)
	HasConfigure bool

	// HasMakeInstall specifies whether the package as an install target in the Makefile
	HasMakeInstall bool

	// ConfigurePreludeCmd is the command to invoke before trying to configure the software
	ConfigurePreludeCmd string
}

func autogen(cfg *Config) error {
	if !cfg.HasAutogen {
		log.Println("-> no autogen.sh script, skipping")
		return nil
	}

	// From here we know that an autogen script is present
	configureScriptPath := filepath.Join(cfg.Source, "configure")
	if util.FileExists(configureScriptPath) {
		log.Println("-> configure script already exists, skipping")
		return nil
	}

	var cmd advexec.Advcmd
	cmd.BinPath = "./autogen.sh"
	targetBin := filepath.Join(cfg.Source, "autogen.sh")

	if !util.FileExists(targetBin) {
		cmd.BinPath = "./autogen.pl"
	}
	cmd.ManifestName = "autogen"
	cmd.ManifestDir = cfg.Install
	cmd.ExecDir = cfg.Source
	cmd.Env = cfg.ConfigureEnv
	res := cmd.Run()
	if res.Err != nil {
		return fmt.Errorf("unable to run autogen from %s, command failed: %w - stdout: %s - stderr: %s", cfg.Source, res.Err, res.Stdout, res.Stderr)
	}

	return nil
}

// MakefileHasTarget checks whether a specific Makefile includes a given target
func (cfg *Config) MakefileHasTarget(target string, path string) bool {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, target+":") {
			return true
		}
	}
	return false
}

// Detect checks what is available from the package in terms of autotools and co.
func (cfg *Config) Detect() {
	if cfg.DetectDone {
		return
	}
	cfg.DetectDone = true
	autogenPath := filepath.Join(cfg.Source, "autogen.sh")
	log.Printf("Checking for %s", autogenPath)
	if util.FileExists(autogenPath) {
		log.Println("... ok")
		cfg.HasAutogen = true
		cfg.HasConfigure = true
		cfg.HasMakeInstall = true
		return
	}
	log.Printf("... not available")

	autogenPerlPath := filepath.Join(cfg.Source, "autogen.pl")
	log.Printf("Checking for %s", autogenPerlPath)
	if util.FileExists(autogenPerlPath) {
		log.Println("... ok")
		cfg.HasAutogen = true
		cfg.HasConfigure = true
		cfg.HasMakeInstall = true
		return
	}
	log.Printf("... not available")

	configurePath := filepath.Join(cfg.Source, "configure")
	log.Printf("checking for %s... ", configurePath)
	if util.FileExists(configurePath) {
		log.Println("... ok")
		cfg.HasConfigure = true
		cfg.HasMakeInstall = true
		return
	}
	log.Println("... not available")

	makefilePath := filepath.Join(cfg.Source, "Makefile")
	log.Printf("checking for %s... ", makefilePath)
	if util.FileExists(makefilePath) {
		log.Printf("... ok")
		cfg.HasMakeInstall = cfg.MakefileHasTarget("install", makefilePath)
		return
	}
	log.Printf("... not available")

	cfg.DetectDone = false
}

// Configure handles the classic configure commands
func (cfg *Config) Configure() error {
	cfg.Detect()

	// Run any configure prelude first
	if cfg.ConfigurePreludeCmd != "" {
		tokens := strings.Split(cfg.ConfigurePreludeCmd, " ")
		cmdBin, err := exec.LookPath(tokens[0])
		if err != nil {
			return fmt.Errorf("unable to run prelude, cannot find %s", tokens[0])
		}

		var preludeCmd advexec.Advcmd
		preludeCmd.BinPath = cmdBin
		preludeCmd.CmdArgs = append(preludeCmd.CmdArgs, tokens[1:]...)
		preludeCmd.ManifestName = "configure_prelude"
		preludeCmd.ManifestDir = cfg.Install
		preludeCmd.ExecDir = cfg.Source
		res := preludeCmd.Run()
		if res.Err != nil {
			return fmt.Errorf("unable to execute configure prelude %s: %w", cfg.ConfigurePreludeCmd, res.Err)
		}
	}

	// Run autogen when necessary
	err := autogen(cfg)
	if err != nil {
		return err
	}

	if !cfg.HasConfigure {
		log.Printf("-> Package does not have configure script, skipping the configuration step\n")
		return nil
	}

	var cmdArgs []string
	if cfg.Install != "" {
		cmdArgs = append(cmdArgs, "--prefix")
		cmdArgs = append(cmdArgs, cfg.Install)
	}
	if len(cfg.ExtraConfigureArgs) > 0 {
		cmdArgs = append(cmdArgs, cfg.ExtraConfigureArgs...)
	}

	configurePath := filepath.Join(cfg.Source, "configure")
	log.Printf("-> Running 'configure': %s %s\n", configurePath, cmdArgs)
	var cmd advexec.Advcmd
	cmd.BinPath = "./configure"
	cmd.ManifestName = "configure"
	cmd.ManifestDir = cfg.Install
	if len(cmdArgs) > 0 {
		cmd.ManifestData = []string{strings.Join(cmdArgs, " ")}
		cmd.CmdArgs = cmdArgs
	}
	cmd.ExecDir = cfg.Source
	cmd.Env = append(cmd.Env, cfg.ConfigureEnv...)
	res := cmd.Run()
	if res.Err != nil {
		return fmt.Errorf("command failed: %s - stdout: %s - stderr: %s", res.Err, res.Stdout, res.Stderr)
	}

	return nil
}
