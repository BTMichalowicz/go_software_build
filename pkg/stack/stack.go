//
// Copyright (c) 2023, NVIDIA CORPORATION. All rights reserved.
//
// See LICENSE.txt for license information
//

package stack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BTMichalowicz/go_software_build/internal/pkg/module"
	"github.com/BTMichalowicz/go_software_build/pkg/builder"
	"github.com/BTMichalowicz/go_util/pkg/util"
)

type StackCfg struct {
	InstallDir string `json:"installDir"`
}

type Component struct {
	Name                  string `json:"name"`
	URL                   string `json:"URL"`
	Branch                string `json:"branch"`
	BranchCheckoutPrelude string `json:"branch_checkout_prelude"`
	ConfigId              string `json:"configure_id"`
	ConfigureDependency   string `json:"configure_dependency"`
	ConfigurePrelude      string `json:"configure_prelude"`
	ConfigureParams       string `json:"configure_params"`
}

type StackDef struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Components []Component
}

type Config struct {
	DefFilePath     string
	ConfigFilePath  string
	Private         bool
	loaded          bool
	BuildEnv        []string
	StackConfig     *StackCfg
	StackDefinition *StackDef
}

const (
	defaultPermission = 0775
)

func (c *Config) Load() error {
	// unmarshale the two configuration files
	defFile, err := os.Open(c.DefFilePath)
	if err != nil {
		return fmt.Errorf("unable to open %s: %w", c.DefFilePath, err)
	}
	defContent, err := ioutil.ReadAll(defFile)
	if err != nil {
		return fmt.Errorf("unable to read the content of %s: %w", c.DefFilePath, err)
	}
	c.StackDefinition = new(StackDef)
	err = json.Unmarshal(defContent, &c.StackDefinition)
	if err != nil {
		return fmt.Errorf("unable to unmarshal content of %s: %w", c.DefFilePath, err)
	}

	cfgFile, err := os.Open(c.ConfigFilePath)
	if err != nil {
		return fmt.Errorf("unable to open %s: %w", c.ConfigFilePath, err)
	}
	cfgContent, err := ioutil.ReadAll(cfgFile)
	if err != nil {
		return fmt.Errorf("unable to read the content of %s: %w", c.ConfigFilePath, err)
	}
	c.StackConfig = new(StackCfg)
	err = json.Unmarshal(cfgContent, &c.StackConfig)
	if err != nil {
		return fmt.Errorf("unable to unmarshal content of %s: %w", c.ConfigFilePath, err)
	}
	c.loaded = true
	return nil
}

func (c *Config) InstallStack() error {
	// A map of all the installed components where the key of the component's name and the value the directory where it is installed
	installedComponents := make(map[string]string)

	// A map of all the identifiers used to configure the different components with dependencies
	configIds := make(map[string]string)

	if !c.loaded {
		err := c.Load()
		if err != nil {
			return fmt.Errorf("unable to load configuration: %w", err)
		}
	}

	if c.StackDefinition.Type == "private" && c.Private {
		return fmt.Errorf("you are trying to install a private stack on a public system, which is strictly prohibited! Please use the -private option if you are on a private system to deploy the target stack")
	}

	if !util.PathExists(c.StackConfig.InstallDir) {
		err := os.MkdirAll(c.StackConfig.InstallDir, defaultPermission)
		if err != nil {
			return fmt.Errorf("unable to create installation directory %s: %w", c.StackConfig.InstallDir, err)
		}
	}

	for _, softwareComponents := range c.StackDefinition.Components {
		// Set a builder
		b := new(builder.Builder)

		stackBasedir := filepath.Join(c.StackConfig.InstallDir, c.StackDefinition.Name)
		if !util.PathExists(stackBasedir) {
			err := os.MkdirAll(stackBasedir, defaultPermission)
			if err != nil {
				return fmt.Errorf("unable to create %s: %w", stackBasedir, err)
			}
		}
		b.Env.ScratchDir = filepath.Join(stackBasedir, "scratch")
		b.Env.InstallDir = filepath.Join(stackBasedir, "install")
		b.Env.BuildDir = filepath.Join(stackBasedir, "build")
		b.Env.SrcDir = filepath.Join(stackBasedir, "src")
		b.Env.Env = c.BuildEnv

		if !util.PathExists(b.Env.ScratchDir) {
			err := os.MkdirAll(b.Env.ScratchDir, defaultPermission)
			if err != nil {
				return fmt.Errorf("unable to create %s: %w", b.Env.ScratchDir, err)
			}
		}

		if !util.PathExists(b.Env.InstallDir) {
			err := os.MkdirAll(b.Env.InstallDir, defaultPermission)
			if err != nil {
				return fmt.Errorf("unable to create %s: %w", b.Env.InstallDir, err)
			}
		}

		if !util.PathExists(b.Env.BuildDir) {
			err := os.MkdirAll(b.Env.BuildDir, defaultPermission)
			if err != nil {
				return fmt.Errorf("unable to create %s: %w", b.Env.BuildDir, err)
			}
		}

		if !util.PathExists(b.Env.SrcDir) {
			err := os.MkdirAll(b.Env.SrcDir, defaultPermission)
			if err != nil {
				return fmt.Errorf("unable to create %s: %w", b.Env.SrcDir, err)
			}
		}

		log.Printf("-> Installing %s", softwareComponents.Name)
		b.App.Name = softwareComponents.Name
		b.App.Source.URL = softwareComponents.URL
		b.App.Source.Branch = softwareComponents.Branch

		if softwareComponents.ConfigureDependency != "" {
			deps := strings.Split(softwareComponents.ConfigureDependency, ",")
			for _, dep := range deps {
				ref := dep
				_, ok := configIds[dep]
				if ok {
					ref = configIds[dep]
				}
				configureOption := fmt.Sprintf("--with-%s=%s", ref, installedComponents[dep])
				b.App.AutotoolsCfg.ExtraConfigureArgs = append(b.App.AutotoolsCfg.ExtraConfigureArgs, configureOption)
			}
		}

		if softwareComponents.ConfigureParams != "" {
			args := strings.Split(softwareComponents.ConfigureParams, " ")
			b.App.AutotoolsCfg.ExtraConfigureArgs = append(b.App.AutotoolsCfg.ExtraConfigureArgs, args...)
		}

		if softwareComponents.ConfigurePrelude != "" {
			b.App.AutotoolsCfg.ConfigurePreludeCmd = softwareComponents.ConfigurePrelude
		}

		if softwareComponents.BranchCheckoutPrelude != "" {
			b.App.Source.BranchCheckoutPrelude = softwareComponents.BranchCheckoutPrelude
		}

		err := b.Load(true)
		if err != nil {
			return fmt.Errorf("unable to load the builder for %s: %w", b.App.Name, err)
		}

		res := b.Install()
		if res.Err != nil {
			return fmt.Errorf("unable to install %s: %w", softwareComponents.Name, res.Err)
		}

		installedComponents[softwareComponents.Name] = filepath.Join(b.Env.InstallDir, softwareComponents.Name)
		if softwareComponents.ConfigId != "" {
			configIds[softwareComponents.Name] = softwareComponents.ConfigId
		}
		log.Printf("-> %s was successfully installed in %s", softwareComponents.Name, b.Env.SrcDir)
	}

	return nil
}

func (c *Config) Export() error {
	err := c.Load()
	if err != nil {
		return fmt.Errorf("c.Load() failed: %w", err)
	}

	stackBasedir := filepath.Join(c.StackConfig.InstallDir, c.StackDefinition.Name)
	if !util.PathExists(stackBasedir) {
		return fmt.Errorf("%s does not exist", stackBasedir)
	}

	installDir := filepath.Join(stackBasedir, "install")
	if !util.PathExists(installDir) {
		return fmt.Errorf("%s does not exist", installDir)
	}

	tarballFilename := c.StackDefinition.Name + ".tar.bz2"
	tarBin, err := exec.LookPath("tar")
	if err != nil {
		return fmt.Errorf("tar is not available: %w", err)
	}
	tarCmd := exec.Command(tarBin, "-cjf", tarballFilename, "install")
	tarCmd.Dir = stackBasedir
	var stderr, stdout bytes.Buffer
	tarCmd.Stderr = &stderr
	tarCmd.Stdout = &stdout
	err = tarCmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %w - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
	}

	fmt.Printf("Stack successfully export: %s\n", filepath.Join(stackBasedir, tarballFilename))
	return nil
}

func (c *Config) Import(filePath string) error {
	err := c.Load()
	if err != nil {
		return fmt.Errorf("c.Load() failed: %w", err)
	}

	stackBasedir := filepath.Join(c.StackConfig.InstallDir, c.StackDefinition.Name)
	if !util.PathExists(stackBasedir) {
		err := os.MkdirAll(stackBasedir, defaultPermission)
		if err != nil {
			return fmt.Errorf("unable to create %s: %w", stackBasedir, err)
		}
	}

	tarBin, err := exec.LookPath("tar")
	if err != nil {
		return fmt.Errorf("ERROR: tar is not available: %w", err)
	}
	tarCmd := exec.Command(tarBin, "-xjf", filePath)
	tarCmd.Dir = stackBasedir
	var stderr, stdout bytes.Buffer
	tarCmd.Stderr = &stderr
	tarCmd.Stdout = &stdout
	err = tarCmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %s - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
	}

	fmt.Printf("Stack successfully import in %s\n", stackBasedir)
	return nil
}

func (c *Config) GenerateModules(copyright, customEnvVarPrefix string) error {
	err := c.Load()
	if err != nil {
		return fmt.Errorf("c.Load() failed: %w", err)
	}

	stackBasedir := filepath.Join(c.StackConfig.InstallDir, c.StackDefinition.Name)
	if !util.PathExists(stackBasedir) {
		return fmt.Errorf("stack base directory %s does not exist", stackBasedir)
	}

	modulefileDir := filepath.Join(stackBasedir, "modulefiles")
	if !util.PathExists(modulefileDir) {
		err := os.MkdirAll(modulefileDir, defaultPermission)
		if err != nil {
			return fmt.Errorf("unable to create %s: %w", modulefileDir, err)
		}
	}

	for _, softwareComponent := range c.StackDefinition.Components {
		var requires []string
		vars := make(map[string]string)
		envVars := make(map[string]string)
		envLayout := make(map[string][]string)

		// Set the requirements
		if softwareComponent.ConfigureDependency != "" {
			deps := strings.Split(softwareComponent.ConfigureDependency, ",")
			requires = append(requires, deps...)
		}

		// Set the vars
		vars["software_stack_dir"] = stackBasedir

		compInstallDir := filepath.Join(stackBasedir, "install", softwareComponent.Name)
		compBinDir := filepath.Join(compInstallDir, "bin")
		compLibDir := filepath.Join(compInstallDir, "lib")
		compIncDir := filepath.Join(compInstallDir, "include")
		compManDir := filepath.Join(compInstallDir, "man")
		compPkgDir := filepath.Join(compLibDir, "pkgconfig")

		// Set the new environment variables
		compBasedirVarName := strings.ToUpper(softwareComponent.Name) + "_DIR"
		compBasedirVarValue := compInstallDir
		envVars[compBasedirVarName] = compBasedirVarValue

		compBuildDir := filepath.Join(stackBasedir, "build", softwareComponent.Name)
		if util.PathExists(compBuildDir) {
			// Figure out the actual directory that was used
			list, err := ioutil.ReadDir(compBuildDir)
			targetDir := ""
			if err != nil {
				return fmt.Errorf("unable to read content of %s: %w", compBuildDir, err)
			}
			for _, entry := range list {
				if util.IsDir(filepath.Join(compBuildDir, entry.Name())) {
					targetDir = entry.Name()
					break
				}
			}
			if targetDir == "" {
				return fmt.Errorf("unable to find build directory from %s: %w", compBuildDir, err)
			}
			compBuildDirVarName := strings.ToUpper(softwareComponent.Name) + "_BUILD_DIR"
			compBuildDirVarValue := filepath.Join(compBuildDir, targetDir)
			envVars[compBuildDirVarName] = compBuildDirVarValue
		} else {
			compSrcDir := filepath.Join(stackBasedir, "src")
			targetDir := ""
			listSrcDirs, err := ioutil.ReadDir(compSrcDir)
			if err == nil {
				// The stack may not have a source directory, for instance when the stack is imported
				// rather than build locally.
				// If the source directory does exist, we set some optional additional environment
				// variables.
				for _, entry := range listSrcDirs {
					if strings.Contains(entry.Name(), softwareComponent.Name) {
						targetDir = entry.Name()
					}
				}
				if targetDir == "" {
					return fmt.Errorf("unable to find build directory from %s: %w", compSrcDir, err)
				}
				compSrcDirVarName := strings.ToUpper(softwareComponent.Name) + "_BUILD_DIR"
				compSrcDirVarValue := filepath.Join(compSrcDir, targetDir)
				envVars[compSrcDirVarName] = compSrcDirVarValue
			}
		}

		// Prepend existing environment variables
		if util.PathExists(compBinDir) {
			envLayout["PATH"] = append(envLayout["PATH"], compBinDir)
		}

		if util.PathExists(compLibDir) {
			envLayout["LIBRARY_PATH"] = append(envLayout["LIBRARY_PATH"], compLibDir)
			envLayout["LD_LIBRARY_PATH"] = append(envLayout["LD_LIBRARY_PATH"], compLibDir)
		}

		if util.PathExists(compIncDir) {
			envLayout["CPATH"] = append(envLayout["CPATH"], compIncDir)
		}

		if util.PathExists(compManDir) {
			envLayout["MANPATH"] = append(envLayout["MANPATH"], compManDir)
		}

		if util.PathExists(compPkgDir) {
			envLayout["PKG_CONFIG_PATH"] = append(envLayout["PKG_CONFIG_PATH"], compPkgDir)
		}

		err := module.Generate(modulefileDir, copyright, customEnvVarPrefix, softwareComponent.Name, requires, nil, vars, envVars, envLayout)
		if err != nil {
			return fmt.Errorf("module.Generate() failed: %w", err)
		}
	}

	fmt.Printf("modules successfully creates, to use them: module use %s\n", modulefileDir)
	return nil
}
