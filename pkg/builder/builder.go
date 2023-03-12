// Copyright (c) 2019, Sylabs Inc. All rights reserved.
// Copyright (c) 2021, NVIDIA CORPORATION. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

// Package builder is a package that provides a set of APIs to help configure, install and uninstall software
// on the host.
package builder

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/BTMichalowicz/go_exec/pkg/advexec"
	"github.com/BTMichalowicz/go_software_build/internal/pkg/autotools"
	"github.com/BTMichalowicz/go_software_build/pkg/app"
	"github.com/BTMichalowicz/go_software_build/pkg/buildenv"
	"github.com/BTMichalowicz/go_util/pkg/util"
)

// GetConfigureExtraArgsFn is the function prootype for getting extra arguments to configure a software
type GetConfigureExtraArgsFn func() []string

// ConfigureFn is the function prototype to configuration a specific software
type ConfigureFn func(*buildenv.Info, string, []string, string) error

// Builder gathers all the data specific to a software builder
type Builder struct {
	// Persistent is the path where to store all the software when we need a persistent install (in opposition to temporary install)
	// Persistent is an empty string when there is no need for a persistent install
	Persistent string

	// SudoRequired specifies if install commands needs to be executed with sudo
	// Note that it is assumed sudo does not require a password, there is no support for interactive password management
	SudoRequired bool

	// Configure is the function to call to configure the software
	Configure ConfigureFn

	// ConfigureExtraArgs is the extra arguments for the configuration command
	ConfigureExtraArgs []string

	// App is the application the builder is associated with
	App app.Info

	// Env is the environment to build/install the software package
	Env buildenv.Info

	// BuildScript is the script to invoke to build the package
	BuildScript string
}

var makefileSpellings = []string{"Makefile", "makefile"}

// GenericConfigure is a generic function to configure a software, basically a wrapper around autotool's configure
func GenericConfigure(env *buildenv.Info, appName string, extraArgs []string, configurePreludeCmd string) error {
	var ac autotools.Config
	ac.Install = filepath.Join(env.InstallDir, appName)
	ac.Source = env.SrcDir
	ac.ConfigureEnv = env.Env
	ac.ExtraConfigureArgs = extraArgs
	ac.ConfigurePreludeCmd = configurePreludeCmd
	err := ac.Configure()
	if err != nil {
		return fmt.Errorf("failed to configure software: %s", err)
	}

	return nil
}

func findMakefile(env *buildenv.Info) (string, []string, error) {
	var makeExtraArgs []string

	for _, makefileSpelling := range makefileSpellings {
		makefilePath := filepath.Join(env.SrcDir, makefileSpelling)
		log.Printf("-> Checking for %s...", makefilePath)
		if !util.FileExists(makefilePath) {
			makefilePath := filepath.Join(env.SrcDir, "builddir", "Makefile")
			if util.FileExists(makefilePath) {
				makeExtraArgs = []string{"-C", "builddir"}
				return makefilePath, makeExtraArgs, nil
			}
		} else {
			return makefilePath, makeExtraArgs, nil
		}
	}

	return "", nil, fmt.Errorf("unable to locate the Makefile")
}

func (b *Builder) compile(pkg *app.Info, env *buildenv.Info) advexec.Result {
	var res advexec.Result
	log.Printf("- Compiling %s...\n", pkg.Name)

	if b.BuildScript != "" {
		destFile := filepath.Join(env.SrcDir, path.Base(b.BuildScript))
		if !util.FileExists(destFile) {
			err := util.CopyFile(b.BuildScript, destFile)
			if err != nil {
				res.Err = err
				return res
			}
			err = os.Chmod(destFile, 0777)
			if err != nil {
				res.Err = err
				return res

			}
		}
		log.Printf("-> Building with %s from %s\n", destFile, env.SrcDir)
		var cmd advexec.Advcmd
		cmd.BinPath = destFile
		cmd.ExecDir = env.SrcDir
		res = cmd.Run()
		return res
	}

	if env.SrcDir == "" {
		res.Err = fmt.Errorf("invalid parameter(s)")
		return res
	}

	pkg.AutotoolsCfg.Detect()
	makefilePath, makeExtraArgs, err := findMakefile(env)
	if err != nil {
		log.Printf("-> No Makefile, trying to figure out how to compile/install %s...", pkg.Name)
		res.Err = fmt.Errorf("failed to figure out how to compile %s", pkg.Name)
		return res
	}

	makefileStage := ""
	res.Err = env.RunMake(false, makefileStage, makefilePath, makeExtraArgs)
	return res
}

func (b *Builder) install(pkg *app.Info, env *buildenv.Info) advexec.Result {
	var res advexec.Result

	if env.InstallDir == "" || env.BuildDir == "" {
		res.Err = fmt.Errorf("invalid parameter(s)")
		return res
	}

	if pkg.AutotoolsCfg.HasMakeInstall {
		// The Makefile has a 'install' target so we just use it
		targetDir := filepath.Join(env.InstallDir, pkg.Name)
		if !util.PathExists(targetDir) {
			err := os.Mkdir(targetDir, 0755)
			if err != nil {
				res.Err = err
				return res
			}
		}

		log.Printf("- Installing %s in %s using 'make install'...", pkg.Name, targetDir)
		makefilePath, makeExtraArgs, err := findMakefile(env)
		if err != nil {
			res.Err = fmt.Errorf("unable to find Makefile: %s", err)
			return res
		}
		res.Err = env.RunMake(b.SudoRequired, "install", makefilePath, makeExtraArgs)
	} else {
		// Copy binaries and libraries to the install directory
		log.Printf("- 'make install' not available, copying files...")
		var cmd advexec.Advcmd
		cmd.BinPath = "cp"
		cmd.CmdArgs = []string{"-rf", env.GetAppBuildDir(pkg), env.InstallDir}
		res := cmd.Run()
		if res.Err != nil {
			return res
		}
	}

	return res
}

// Install installs a software package on the host
func (b *Builder) Install() advexec.Result {
	var res advexec.Result

	// Sanity checks
	if b.Env.InstallDir == "" {
		res.Err = fmt.Errorf("undefined install directory")
		return res
	}
	if b.App.Source.URL == "" {
		res.Err = fmt.Errorf("undefined application's URL")
		return res
	}

	log.Printf("Installing %s on host...", b.App.Name)
	appInstallDir := b.Env.GetAppInstallDir(&b.App)
	if b.Persistent != "" {
		if b.Env.InstallDir != b.Persistent {
			log.Printf("* Updating install directory from %s default to %s\n", b.Env.InstallDir, b.Persistent)
			b.Env.InstallDir = b.Persistent
		}
		appInstallDir = b.Env.GetAppInstallDir(&b.App)
	}
	if util.PathExists(appInstallDir) {
		log.Printf("* %s already exists, skipping installation...", appInstallDir)
		b.Env.SrcDir = appInstallDir
		return res
	}

	log.Printf("* %s does not exists, installing from scratch\n", appInstallDir)

	res.Err = b.Env.Get(&b.App)
	if res.Err != nil {
		res.Err = fmt.Errorf("failed to download software from %s: %s", b.App.Source.URL, res.Err)
		return res
	}
	if b.Env.SrcPath == "" {
		res.Err = fmt.Errorf("failed to get a path to the source")
		return res
	}

	res.Err = b.Env.Unpack(&b.App)
	if res.Err != nil {
		res.Err = fmt.Errorf("failed to unpack %s: %s", b.App.Name, res.Err)
		return res
	}

	b.App.AutotoolsCfg.Source = b.Env.SrcDir
	b.App.AutotoolsCfg.Detect()

	// Right now, we assume we do not have to install autotools, which is a bad assumption
	var extraArgs []string
	if len(b.App.AutotoolsCfg.ExtraConfigureArgs) > 0 {
		extraArgs = append(extraArgs, b.App.AutotoolsCfg.ExtraConfigureArgs...)
	}
	res.Err = b.Configure(&b.Env, b.App.Name, extraArgs, b.App.AutotoolsCfg.ConfigurePreludeCmd)
	if res.Err != nil {
		res.Err = fmt.Errorf("failed to configure %s: %s", b.App.Name, res.Err)
		return res
	}

	res = b.compile(&b.App, &b.Env)
	if res.Err != nil {
		res.Stderr = fmt.Sprintf("failed to compile %s: %s", b.App.Name, res.Err)
		return res
	}

	res = b.install(&b.App, &b.Env)
	if res.Err != nil {
		res.Stderr = fmt.Sprintf("failed to install software: %s", res.Err)
		return res
	}

	return res
}

// Uninstall uninstalls a version of software from the host that was previously installed by our tool
func (b *Builder) Uninstall() advexec.Result {
	var res advexec.Result
	if b.Persistent == "" {
		if util.PathExists(b.Env.InstallDir) {
			err := os.RemoveAll(b.Env.InstallDir)
			if err != nil {
				res.Err = err
				return res
			}
		}
	} else {
		log.Printf("Persistent installs mode, not uninstalling software from host")
	}

	return res
}

// Load is the function that will figure out the function to call for various stages of the code configuration/compilation/installation/execution
func (b *Builder) Load(persistent bool) error {
	// fixme: at this point, we know the app and we have the builder object
	// so we should be able to do a autodetect instead of forcing autotools
	b.Configure = GenericConfigure

	if b.App.Name == "" {
		return fmt.Errorf("application's name is undefined")
	}

	if b.App.Source.URL == "" {
		return fmt.Errorf("the URL to download application is undefined")
	}

	if b.Env.ScratchDir == "" {
		return fmt.Errorf("scratch directory is undefined")
	}

	if b.Env.BuildDir == "" {
		return fmt.Errorf("build directory is undefined")
	}

	if b.Env.InstallDir == "" {
		return fmt.Errorf("install directory is undefined")
	}

	return nil
}

// Compile compiles and installs a given application on the host
func (b *Builder) Compile() error {
	// The builder has a general environment (set by caller) but we need a detailed
	// environment specific to the app
	var buildEnv buildenv.Info
	buildEnv.BuildDir = filepath.Join(b.Env.ScratchDir, b.App.Name)
	buildEnv.InstallDir = filepath.Join(b.Env.InstallDir, b.App.Name)
	buildEnv.SrcPath = filepath.Join(b.Env.SrcDir, filepath.Base(b.App.Source.URL))

	if !util.PathExists(buildEnv.BuildDir) {
		err := util.DirInit(buildEnv.BuildDir)
		if err != nil {
			return fmt.Errorf("failed to initialize directory %s: %s", buildEnv.BuildDir, err)
		}
	}
	if !util.PathExists(buildEnv.InstallDir) {
		err := util.DirInit(buildEnv.InstallDir)
		if err != nil {
			return fmt.Errorf("failed to initialize directory %s: %s", buildEnv.InstallDir, err)
		}
	}

	log.Printf("Build the application in %s\n", buildEnv.BuildDir)
	log.Printf("Install the application in %s\n", buildEnv.InstallDir)

	// Download the app
	err := buildEnv.Get(&b.App)
	if err != nil {
		return fmt.Errorf("unable to get the application from %s: %s", b.App.Source.URL, err)
	}

	// Unpacking the app
	err = buildEnv.Unpack(&b.App)
	if err != nil {
		return fmt.Errorf("unable to unpack the application %s: %s", buildEnv.SrcPath, err)
	}

	// Install the app
	log.Println("-> Building the application...")
	err = buildEnv.Install(&b.App)
	if err != nil {
		return fmt.Errorf("unable to install package: %s", err)
	}

	// todo: we do not have a good way to know if an app is actually install in InstallDir or
	// if we must just use the binary in BuildDir. For now we assume that we use the binary in
	// BuildDir.
	b.App.BinPath = filepath.Join(buildEnv.SrcDir, b.App.BinName)
	log.Printf("-> Successfully created %s\n", b.App.BinPath)

	return nil
}
