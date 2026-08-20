package jetbrains

import "encoding/xml"

// The structs below mirror JetBrains' run-configuration XML shape
// closely enough for the fields godev actually needs; unrecognized
// elements are silently ignored by encoding/xml, which is the
// intended, forward-compatible behavior here.

type xmlComponent struct {
	XMLName        xml.Name        `xml:"component"`
	Configurations []xmlConfigItem `xml:"configuration"`
}

type xmlConfigItem struct {
	Name       string `xml:"name,attr"`
	Type       string `xml:"type,attr"`
	FolderName string `xml:"folderName,attr"`

	// Go (GoApplicationRunConfiguration)
	WorkingDirectory *xmlValue `xml:"working_directory"`
	Parameters       *xmlValue `xml:"parameters"`

	// Node.js (NodeJSConfigurationType)
	WorkingDir *xmlValue `xml:"working-dir"`
	Path       *xmlValue `xml:"path"`

	// npm (js.build_tools.npm)
	PackageJSON *xmlValue   `xml:"package-json"`
	Command     *xmlValue   `xml:"command"`
	Scripts     *xmlScripts `xml:"scripts"`

	// Shell script (ShConfigurationType)
	Options []xmlOption `xml:"option"`

	// Shared
	Envs *xmlEnvs `xml:"envs"`
}

type xmlValue struct {
	Value string `xml:"value,attr"`
}

type xmlScripts struct {
	Script []xmlValue `xml:"script"`
}

type xmlEnvs struct {
	Env []xmlEnv `xml:"env"`
}

type xmlEnv struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type xmlOption struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
