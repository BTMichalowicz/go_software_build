// Copyright (c) 2019, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package app

import "github.com/BTMichalowicz/go_software_build/internal/pkg/autotools"

type SourceCode struct {
	// URL is the url to use to download the app
	URL string

	// Branch is the specific flavor of the code to use. Directly applicable to git for example
	Branch string

	// Command to execute before checking out a branch
	BranchCheckoutPrelude string
}

// Info gathers information about a given application
type Info struct {
	// Name is the name of the application
	Name string

	// Information about the source code of the applicatin
	Source SourceCode

	// BinName is the name of the binary to start executing the application
	BinName string

	// BinPath is the path to the binary to start executing the application
	BinPath string

	// BinArgs is the list of argument that the application's binary needs
	BinArgs []string

	// InstallCmd is the command to execute to install the app (in case it is not a standard command)
	InstallCmd string

	// Version is the version of the application to concider
	Version string

	// Tarball is the name of the tarball of the application
	Tarball string

	// AutotoolsCfg is the autotools' configuration of the package, used to know how to configure, compile and install the software package
	AutotoolsCfg autotools.Config
}
