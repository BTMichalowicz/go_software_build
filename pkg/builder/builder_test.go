// Copyright (c) 2021, NVIDIA CORPORATION. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package builder

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/BTMichalowicz/go_exec/pkg/advexec"
	"github.com/BTMichalowicz/go_util/pkg/util"
)

func setBuilder(t *testing.T) (*Builder, func()) {
	b := new(Builder)

	var err error
	b.Env.ScratchDir, err = ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create scratch directory: %s", err)
	}
	t.Logf("Scratch directory: %s", b.Env.ScratchDir)

	b.Env.InstallDir, err = ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create install directory: %s", err)
	}
	t.Logf("Install directory: %s", b.Env.InstallDir)

	b.Env.BuildDir, err = ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create build directory: %s", err)
	}
	t.Logf("Build directory: %s", b.Env.BuildDir)

	// Create a directory where software is downloaded
	b.Env.SrcDir, err = ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create src directory: %s", err)
	}
	t.Logf("Src directory: %s", b.Env.SrcDir)

	cleanupFn := func() {
		os.RemoveAll(b.Env.ScratchDir)
		os.RemoveAll(b.Env.InstallDir)
		os.RemoveAll(b.Env.BuildDir)
		os.RemoveAll(b.Env.SrcDir)
	}

	return b, cleanupFn
}

func TestInstallFromAutotoolsRelease(t *testing.T) {
	b, cleanupFn := setBuilder(t)
	defer cleanupFn()
	t.Logf("Build directory: %s", b.Env.BuildDir)
	t.Logf("Source directory: %s", b.Env.SrcDir)
	b.App.Name = "c_hello_world"
	b.App.Source.URL = "https://github.com/BTMichalowicz/c_hello_world/releases/download/v1.0.1/c_hello_world-1.0.1.tar.gz"
	b.App.Version = "1.0.1"

	err := b.Load(false)
	if err != nil {
		t.Fatalf("unable to load the builder: %s", err)
	}

	if b.Configure == nil {
		t.Fatalf("builder configure is undefined")
	}

	res := b.Install()
	if res.Err != nil {
		t.Fatalf("unable to install the software package: %s", res.Err)
	}

	expectedBinary := filepath.Join(b.Env.InstallDir, b.App.Name, "bin", "helloworld")
	t.Logf("Checking if %s was correctly installed,,,", expectedBinary)
	if !util.FileExists(expectedBinary) {
		t.Fatalf("expected binary %s does not exist", expectedBinary)
	}

}

func TestBuilderEnv(t *testing.T) {
	b, cleanupFn := setBuilder(t)
	defer cleanupFn()
	t.Logf("Build directory: %s", b.Env.BuildDir)
	t.Logf("Source directory: %s", b.Env.SrcDir)
	b.App.Name = "helloworld"
	b.App.Source.URL = "https://github.com/BTMichalowicz/c_hello_world/archive/1.0.0.tar.gz"
	b.App.Version = "1.0.0"
	b.Env.Env = append(b.Env.Env, "CC=/dummy/toto")

	err := b.Load(false)
	if err != nil {
		t.Fatalf("unable to load the builder: %s", err)
	}

	if b.Configure == nil {
		t.Fatalf("builder configure is undefined")
	}

	res := b.Install()
	if res.Err == nil {
		t.Fatalf("install succeeded even when specifying an invalid C compiler through the environment")
	}
}

func TestInstallFromSource(t *testing.T) {
	b, cleanupFn := setBuilder(t)
	defer cleanupFn()

	t.Logf("Build directory: %s", b.Env.BuildDir)
	t.Logf("Source directory: %s", b.Env.SrcDir)
	b.App.Name = "helloworld"
	b.App.Source.URL = "https://github.com/BTMichalowicz/c_hello_world/archive/1.0.0.tar.gz"
	b.App.Version = "1.0.0"

	err := b.Load(false)
	if err != nil {
		t.Fatalf("unable to load the builder: %s", err)
	}

	if b.Configure == nil {
		t.Fatalf("builder configure is undefined")
	}

	res := b.Install()
	if res.Err != nil {
		t.Fatalf("unable to install the software package: %s", res.Err)
	}

	expectedBinary := filepath.Join(b.Env.InstallDir, b.App.Name, "bin", "helloworld")
	t.Logf("Checking if %s was correctly installed,,,", expectedBinary)
	if !util.FileExists(expectedBinary) {
		t.Fatalf("expected binary %s does not exist", expectedBinary)
	}
}

func TestPersistentBuildFromLocalTarball(t *testing.T) {
	url := "https://github.com/BTMichalowicz/c_hello_world/archive/1.0.0.tar.gz"
	tarballFilename := "1.0.0.tar.gz"
	wgetBin, err := exec.LookPath("wget")
	if err != nil {
		t.Skip("wget not available skipping test")
	}

	downloadDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create temporary directory: %s", err)
	}
	defer os.RemoveAll(downloadDir)

	downloadCmd := new(advexec.Advcmd)
	downloadCmd.BinPath = wgetBin
	downloadCmd.CmdArgs = []string{url}
	downloadCmd.ExecDir = downloadDir
	res := downloadCmd.Run()
	if res.Err != nil {
		t.Fatalf("unable to download tarball")
	}

	downloadedTarball := filepath.Join(downloadDir, tarballFilename)
	if !util.FileExists(downloadedTarball) {
		t.Fatalf("unable to find tarball (expected: %s)", downloadedTarball)
	}
	t.Logf("Tarball location: %s\n", downloadedTarball)

	b, cleanupFn := setBuilder(t)
	defer cleanupFn()

	b.App.Name = "test"
	b.App.Source.URL = "file://" + filepath.Join(downloadDir, tarballFilename)
	err = b.Load(true)
	if err != nil {
		t.Fatalf("unable to load builder: %s", err)
	}

	res = b.Install()
	if res.Err != nil {
		t.Fatalf("unable to install test tarball: %s", res.Err)
	}

	if b.Env.SrcPath == "" {
		t.Fatalf("SrcPath is undefined")
	}

	// The tarball being available locally, it should have been copied directly into the build directory
	targetFile := filepath.Join(b.Env.BuildDir, b.App.Name, tarballFilename)
	if !util.FileExists(targetFile) {
		t.Fatalf("expected file %s does not exist", targetFile)
	}

	expectedBinary := filepath.Join(b.Env.InstallDir, b.App.Name, "bin", "helloworld")
	t.Logf("Checking if %s was correctly installed,,,", expectedBinary)
	if !util.FileExists(expectedBinary) {
		t.Fatalf("expected binary %s does not exist", expectedBinary)
	}

	expectedTarball := filepath.Join(b.Env.BuildDir, b.App.Name, tarballFilename)
	if b.Env.SrcPath != expectedTarball {
		t.Fatalf("expected tarball is missing: %s instead of %s", b.Env.SrcPath, expectedTarball)
	}
}
