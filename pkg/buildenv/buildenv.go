// Copyright (c) 2019, Sylabs Inc. All rights reserved.
// Copyright (c) 2021, NVIDIA CORPORATION. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

// Package buildenv is a package that provides all the capabilities to deal with a build environment,
// from defining where the software should be compiled and install, to the actual configuration,
// compilation and installation of software.
package buildenv

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/BTMichalowicz/go_exec/pkg/advexec"
	"github.com/BTMichalowicz/go_software_build/pkg/app"
	"github.com/BTMichalowicz/go_util/pkg/util"
)

const (
	defaultDirMode = 0755
)

// Info gathers the details of the build environment
type Info struct {
	// SrcPath is the path to the downloaded tarball
	// This value is set by the tool after getting the package's source code
	SrcPath string

	// SrcDir is the directory where the source code is
	// This value may be updated  by the tool after getting the package's source code
	SrcDir string

	// ScratchDir is the directory where we can store temporary data
	// This value is part of the build environment configuration
	ScratchDir string

	// InstallDir is the directory where the software needs to be installed
	// This value is part of the build environment configuration
	InstallDir string

	// BuildDir is the directory where the software is built
	// This value is part of the build environment configuration
	BuildDir string

	// Env is the environment to use with the build environment
	Env []string

	// ConfigureExtraArgs is the extra arguments to use when running configure
	ConfigureExtraArgs []string

	// MakeExtraArgs is the extra arguments to use when running make
	MakeExtraArgs []string
}

// Unpack extracts the source code from a package/tarball/zip file.
func (env *Info) Unpack(appInfo *app.Info) error {
	log.Println("- Unpacking software...")

	// Sanity checks
	if env.SrcPath == "" {
		return fmt.Errorf("env.SrcPath is undefined")
	}

	if env.BuildDir == "" {
		return fmt.Errorf("env.BuildDir is undefined")
	}

	srcObject := filepath.Join(env.SrcDir, appInfo.Tarball)
	/*
		if util.IsDir(env.SrcDir) {
			// If we point to a directory, it is something like a Git checkout so nothing to do
			log.Printf("%s does not seem to need to be unpacked (directory), skipping...", env.SrcPath)
			return nil
		}
	*/

	// Figure out the extension of the tarball
	format := util.DetectTarballFormat(srcObject)
	if format == "" {
		// A typical use case here is a single file that just needs to be compiled
		log.Printf("%s does not seem to need to be unpacked (unsupported format?), skipping...", env.SrcDir)
		return nil
	}

	// At the moment we always assume we have to use the tar command
	// (and it is a fair assumption for our current context)
	tarPath, err := exec.LookPath("tar")
	if err != nil {
		return fmt.Errorf("tar is not available: %w", err)
	}

	tarArg := util.GetTarArgs(format)
	if tarArg == "" {
		return fmt.Errorf("unsupported format: %s", format)
	}

	// Untar the package into the build directory
	log.Printf("-> Executing from %s: %s %s %s \n", env.SrcDir, tarPath, tarArg, srcObject)
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(tarPath, tarArg, srcObject)
	cmd.Dir = env.SrcDir
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %w - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
	}

	// We save the directory created while untaring the tarball
	entries, err := ioutil.ReadDir(env.SrcDir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", env.SrcDir, err)
	}
	// The source directory now has at most 2 entries: the tarball and the directory resulting from unpacking it
	if len(entries) > 2 {
		listDirs := ""
		for _, e := range entries {
			listDirs = e.Name() + ","
		}
		return fmt.Errorf("inconsistent temporary %s directory, %d files instead of 1 or 2: %s", env.SrcDir, len(entries), listDirs)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(appInfo.Tarball) {
			env.SrcDir = filepath.Join(env.SrcDir, e.Name())
			break
		}
	}
	log.Printf("-> SrcDir is now %s", env.SrcDir)

	return nil
}

// RunMake executes the appropriate command to build the software
func (env *Info) RunMake(sudo bool, stage string, makefilePath string, args []string) error {
	// Some sanity checks
	if env.SrcDir == "" {
		return fmt.Errorf("env.SrcDir is undefined")
	}

	var makeCmd advexec.Advcmd
	makeCmd.ManifestName = "make"
	if stage != "" {
		args = append(args, stage)
		makeCmd.ManifestName = strings.Join(args, "_")
	}

	args = append([]string{"-j"}, args...)
	logMsg := "make " + strings.Join(args, " ")
	if !sudo {
		makeCmd.BinPath = "make"
	} else {
		sudoBin, err := exec.LookPath("sudo")
		logMsg = sudoBin + " " + logMsg
		if err != nil {
			return fmt.Errorf("failed to find the sudo binary: %s", err)
		}
		args = append([]string{"make"}, args...)
		makeCmd.BinPath = sudoBin
	}
	makeCmd.CmdArgs = append(makeCmd.CmdArgs, args...)
	makeCmd.CmdArgs = append(makeCmd.CmdArgs, env.MakeExtraArgs...)
	log.Printf("* Executing (from %s): %s", env.SrcDir, logMsg)
	if len(env.Env) > 0 {
		log.Printf("-> Using env: %s\n", env.Env)
		makeCmd.Env = env.Env
	}
	makeCmd.ExecDir = filepath.Dir(makefilePath)
	res := makeCmd.Run()
	if res.Err != nil {
		return fmt.Errorf("command failed: %w - stdout: %s - stderr: %s", res.Err, res.Stdout, res.Stderr)
	}

	return nil
}

// CopyTarball copies a tarball to a build directory
func (env *Info) copyTarball(p *app.Info) error {
	// Some sanity checks
	if p.Source.URL == "" {
		return fmt.Errorf("invalid copyTarball() parameter(s)")
	}

	// Figure out the name of the file if we do not already have it
	if p.Tarball == "" {
		p.Tarball = path.Base(p.Source.URL)
	}

	targetDir := filepath.Join(env.BuildDir, p.Name)
	if !util.PathExists(targetDir) {
		err := os.MkdirAll(targetDir, defaultDirMode)
		if err != nil {
			return err
		}
	}
	targetTarballPath := filepath.Join(targetDir, p.Tarball)

	if util.FileExists(targetTarballPath) {
		log.Printf("%s already exists, not copying", targetTarballPath)
	} else {
		// The begining of the URL starts with 'file://' which we do not want
		err := util.CopyFile(p.Source.URL[7:], targetTarballPath)
		if err != nil {
			return fmt.Errorf("cannot copy file %s to %s: %w", p.Source.URL, targetTarballPath, err)
		}
	}

	env.SrcDir = targetDir
	env.SrcPath = targetTarballPath

	return nil
}

func (env *Info) gitCheckout(p *app.Info) error {
	// todo: should it be cached in sysCfg and passed in?
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("failed to find git: %w", err)
	}

	repoName := filepath.Base(p.Source.URL)
	repoName = strings.Replace(repoName, ".git", "", 1)
	targetDir := filepath.Join(env.BuildDir, p.Name)
	if !util.PathExists(targetDir) {
		err = os.MkdirAll(targetDir, defaultDirMode)
		if err != nil {
			return err
		}
	}
	checkoutPath := filepath.Join(targetDir, repoName)

	if util.PathExists(checkoutPath) {
		gitCmd := exec.Command(gitBin, "pull")
		log.Printf("Running from %s: %s pull\n", checkoutPath, gitBin)
		gitCmd.Dir = checkoutPath
		var stderr, stdout bytes.Buffer
		gitCmd.Stderr = &stderr
		gitCmd.Stdout = &stdout
		err = gitCmd.Run()
		if err != nil {
			return fmt.Errorf("command failed: %w - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
		}
	} else {
		gitCloneCmd := exec.Command(gitBin, "clone", p.Source.URL)
		log.Printf("Running from %s: %s clone %s\n", env.BuildDir, gitBin, p.Source.URL)
		gitCloneCmd.Dir = targetDir
		var stderr, stdout bytes.Buffer
		gitCloneCmd.Stderr = &stderr
		gitCloneCmd.Stdout = &stdout
		err = gitCloneCmd.Run()
		if err != nil {
			return fmt.Errorf("command failed: %w - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
		}

		if p.Source.BranchCheckoutPrelude != "" {
			tokens := strings.Split(p.Source.BranchCheckoutPrelude, " ")
			cmdBin, err := exec.LookPath(tokens[0])
			if err != nil {
				return fmt.Errorf("unable to run prelude before checking out the branch, cannot find %s", tokens[0])
			}

			gitCheckoutPreludeCmd := exec.Command(cmdBin, tokens[1:]...)
			log.Printf("Running from %s: %s %s\n", env.BuildDir, cmdBin, strings.Join(tokens[1:], " "))
			gitCheckoutPreludeCmd.Dir = filepath.Join(targetDir, repoName)
			gitCheckoutPreludeCmd.Stderr = &stderr
			gitCheckoutPreludeCmd.Stdout = &stdout
			err = gitCheckoutPreludeCmd.Run()
			if err != nil {
				return fmt.Errorf("command failed: %s - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
			}
		}

		if p.Source.Branch != "" {
			gitCheckoutCmd := exec.Command(gitBin, "checkout", p.Source.Branch)
			log.Printf("Running from %s: %s checkout %s\n", env.BuildDir, gitBin, p.Source.Branch)
			gitCheckoutCmd.Dir = filepath.Join(targetDir, repoName)
			gitCheckoutCmd.Stderr = &stderr
			gitCheckoutCmd.Stdout = &stdout
			err = gitCheckoutCmd.Run()
			if err != nil {
				return fmt.Errorf("command failed: %s - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
			}
		}
	}

	// Both env.SrcPath and env.SrcDir are set to the directory checkout because:
	// - the value of SrcPath will make the code figure out in a safe manner that it is not necessary to do unpack
	// - the value of SrcDir will point to where the code is from configuration/compilation/installation
	env.SrcPath = checkoutPath
	env.SrcDir = checkoutPath

	return nil
}

// Get is the function to get a given source code
func (env *Info) Get(p *app.Info) error {
	log.Printf("- Getting %s from %s...\n", p.Name, p.Source.URL)

	// Sanity checks
	if p.Source.URL == "" {
		return fmt.Errorf("invalid Get() parameter(s)")
	}

	// Detect the type of URL, e.g., file vs. http*
	urlFormat := util.DetectURLType(p.Source.URL)
	if urlFormat == "" {
		return fmt.Errorf("impossible to detect type from URL %s", p.Source.URL)
	}

	switch urlFormat {
	case util.FileURL:
		path := p.Source.URL[7:]
		if !util.IsDir(path) {
			err := env.copyTarball(p)
			if err != nil {
				return fmt.Errorf("env.copyTarball() failed: %w", err)
			}
		} else {
			// If we deal with a directory, we always copy it directly to the build directory because
			// it is a pain to safely cache
			targetDir := filepath.Join(env.BuildDir, p.Name)
			if !util.PathExists(targetDir) {
				err := os.MkdirAll(targetDir, 0755)
				if err != nil {
					return err
				}
			}
			var cmd advexec.Advcmd
			var err error
			cmd.BinPath, err = exec.LookPath("cp")
			if err != nil {
				return fmt.Errorf("cp command not available")
			}
			cmd.CmdArgs = append(cmd.CmdArgs, "-rf")
			cmd.CmdArgs = append(cmd.CmdArgs, path)
			cmd.CmdArgs = append(cmd.CmdArgs, targetDir)
			res := cmd.Run()
			if res.Err != nil {
				return fmt.Errorf("unable to copy %s into %s: %w, stdout: %s, stderr: %s", path, targetDir, res.Err, res.Stdout, res.Stderr)
			}

			env.SrcPath = filepath.Join(targetDir, filepath.Base(p.Source.URL))
			env.SrcDir = env.SrcPath
		}
	case util.HttpURL:
		err := env.download(p)
		if err != nil {
			return fmt.Errorf("env.download() failed, impossible to download %s: %w", p.Name, err)
		}
	case util.GitURL:
		// If we deal with a Git repository, we always clone it in the build directory because
		// it is a pain to safely cache
		env.SrcPath = env.BuildDir
		err := env.gitCheckout(p)
		if err != nil {
			return fmt.Errorf("impossible to get Git repository %s: %s", p.Source.URL, err)
		}
	default:
		return fmt.Errorf("impossible to detect URL type: %s", p.Source.URL)
	}

	return nil
}

func (env *Info) download(p *app.Info) error {
	// Sanity checks
	if p.Source.URL == "" {
		return fmt.Errorf("p.URL is undefined")
	}

	if env.SrcDir == "" {
		return fmt.Errorf("env.SrcPath is undefined")
	}

	if !util.PathExists(env.SrcDir) {
		err := os.Mkdir(env.SrcDir, defaultDirMode)
		if err != nil {
			return err
		}
	}
	if p.Tarball == "" {
		p.Tarball = filepath.Base(p.Source.URL)
	}
	targetFile := filepath.Join(env.SrcDir, p.Tarball)
	if util.FileExists(targetFile) {
		log.Printf("- %s already exists, not downloading...", targetFile)
	} else {
		log.Printf("- Downloading %s from %s into %s...", p.Name, p.Source.URL, env.SrcDir)

		// todo: do not assume wget
		binPath, err := exec.LookPath("wget")
		if err != nil {
			return fmt.Errorf("cannot find wget: %s", err)
		}

		log.Printf("* Executing from %s: %s %s", env.SrcDir, binPath, p.Source.URL)
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(binPath, p.Source.URL)
		cmd.Dir = env.SrcDir
		cmd.Stderr = &stderr
		cmd.Stdout = &stdout
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("command failed: %w - stdout: %s - stderr: %s", err, stdout.String(), stderr.String())
		}
	}
	env.SrcPath = targetFile

	return nil
}

func getNameFromFilename(filename string) string {
	format := util.DetectTarballFormat(filename)
	if format == util.FormatBZ2 {
		filename = strings.TrimSuffix(filename, "."+util.FormatBZ2)
		format = util.DetectTarballFormat(filename)
	}
	if format == util.FormatGZ {
		filename = strings.TrimSuffix(filename, "."+util.FormatGZ)
		format = util.DetectTarballFormat(filename)
	}
	if format == util.FormatTGZ {
		filename = strings.TrimSuffix(filename, "."+util.FormatTGZ)
		format = util.DetectTarballFormat(filename)
	}
	if format == util.FormatTAR {
		filename = strings.TrimSuffix(filename, "."+util.FormatTAR)
	}
	return filename
}

func (env *Info) getAppInstallDirFromURL(a *app.Info) string {
	switch util.DetectURLType(a.Source.URL) {
	case util.FileURL:
		filename := path.Base(a.Source.URL)
		filename = getNameFromFilename(filename)
		return filepath.Join(env.InstallDir, filename)
	case util.HttpURL:
		// todo: do not assume that a package downloaded from the web is always a tarball
		filename := path.Base(a.Source.URL)
		filename = getNameFromFilename(filename)
		return filepath.Join(env.InstallDir, filename)
	case util.GitURL:
		dirname := path.Base(a.Source.URL)
		dirname = strings.Replace(dirname, ".git", "", -1)
		return filepath.Join(env.InstallDir, dirname)
	}

	return ""
}

func (env *Info) getTargetDir(basedir string, a *app.Info) string {
	if a.Name != "" {
		return filepath.Join(basedir, a.Name)
	}
	if a.Source.URL != "" {
		return env.getAppInstallDirFromURL(a)
	}
	return ""

}

// GetAppInstallDir returns the full path where a specific application is to be installed
func (env *Info) GetAppInstallDir(a *app.Info) string {
	return env.getTargetDir(env.InstallDir, a)
}

// GetAppBuildDir returns the full path where a specific application is to be installed
func (env *Info) GetAppBuildDir(a *app.Info) string {
	return env.getTargetDir(env.BuildDir, a)
}

// IsInstalled checks whether a specific software package is already installed in a specific build environment
func (env *Info) IsInstalled(p *app.Info) bool {
	installDir := env.GetAppInstallDir(p)
	return util.PathExists(installDir)
}

// GetEnvPath returns the string representing the value for the PATH environment
// variable to use
func (env *Info) GetEnvPath() string {
	return filepath.Join(env.InstallDir, "bin") + ":" + os.Getenv("PATH")
}

// GetEnvLDPath returns the string representing the value for the LD_LIBRARY_PATH
// environment variable to use
func (env *Info) GetEnvLDPath() string {
	return filepath.Join(env.InstallDir, "lib") + ":" + os.Getenv("LD_LIBRARY_PATH")
}

func (env *Info) lookPath(bin string) string {
	for _, e := range env.Env {
		envEntry := strings.Split(e, "=")
		if envEntry[0] == "PATH" {
			tokens := strings.Split(envEntry[1], ":")
			for _, t := range tokens {
				fullPath := filepath.Join(t, bin)
				if util.FileExists(fullPath) {
					return fullPath
				}
			}
		}
	}

	return bin
}

// Install is a generic function to install a software
func (env *Info) Install(p *app.Info) error {
	if p.InstallCmd == "" {
		log.Println("* Application does not need installation, skipping...")
		return nil
	}

	var cmd advexec.Advcmd
	cmdElts := strings.Split(p.InstallCmd, " ")
	cmd.BinPath = env.lookPath(cmdElts[0])
	cmd.CmdArgs = cmdElts[1:]
	cmd.ExecDir = env.SrcDir
	cmd.ManifestName = "install"
	cmd.ManifestDir = env.InstallDir
	cmd.Env = env.Env

	log.Printf("Executing from %s: %s %s.", env.SrcDir, cmd.BinPath, strings.Join(cmdElts[1:], " "))
	log.Printf("Environment: %s\n", strings.Join(env.Env, "\n"))
	res := cmd.Run()
	if res.Err != nil {
		return fmt.Errorf("failed to install %s: %s; stdout: %s; stderr: %s", p.Name, res.Err, res.Stdout, res.Stderr)
	}

	return nil
}

// Init ensures that the buildenv is correctly initialized
func (env *Info) Init() error {
	if !util.PathExists(env.ScratchDir) {
		err := os.MkdirAll(env.ScratchDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create scratch directory %s: %w", env.ScratchDir, err)
		}
	}
	if !util.PathExists(env.BuildDir) {
		err := os.MkdirAll(env.BuildDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create build directory %s: %w", env.BuildDir, err)
		}
	}
	if !util.PathExists(env.InstallDir) {
		err := os.MkdirAll(env.InstallDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create build directory %s: %w", env.InstallDir, err)
		}
	}
	return nil
}
