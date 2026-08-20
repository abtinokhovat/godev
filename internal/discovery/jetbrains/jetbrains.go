// Package jetbrains imports run configurations a developer already
// defined in a JetBrains IDE (GoLand, WebStorm, IntelliJ, etc.) from
// .idea/runConfigurations/*.xml, so they don't have to be re-entered
// into .godev.yaml by hand. This is read-only: godev never writes back
// into .idea/, to avoid corrupting JetBrains' own state or fighting a
// second writer.
//
// Only a handful of well-known configuration types are understood; any
// other type is silently ignored rather than guessed at.
package jetbrains

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

// projectDirMacro is the placeholder JetBrains uses in run-configuration
// XML for the project root; godev resolves it to the real path.
const projectDirMacro = "$PROJECT_DIR$"

// RunConfig is a normalized run configuration imported from a
// JetBrains .run XML file.
//
// Go configurations carry no Command: they're an enrichment for a
// service godev's own `go list` discovery already found (matched by
// Directory), supplying Args/Env/Group on top. Every other recognized
// type carries an explicit Command and becomes a standalone,
// Command-based service in its own right, exactly like a manual
// .godev.yaml entry.
type RunConfig struct {
	Name      string
	IsGo      bool
	Directory string
	Command   []string
	Args      []string
	Env       map[string]string
	Group     []string
}

// Import scans .idea/runConfigurations/*.xml under projectRoot. A
// missing or absent directory is not an error - it just means there's
// nothing to import, same as a missing .godev.yaml.
func Import(projectRoot string) ([]RunConfig, error) {
	dir := filepath.Join(projectRoot, ".idea", "runConfigurations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var configs []RunConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".xml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var component xmlComponent
		if err := xml.Unmarshal(data, &component); err != nil {
			// A malformed file (or one JetBrains itself doesn't fully
			// populate yet) shouldn't block every other import.
			continue
		}
		for _, raw := range component.Configurations {
			if rc, ok := convert(raw, projectRoot); ok {
				configs = append(configs, rc)
			}
		}
	}
	return configs, nil
}

func resolvePath(projectRoot, value string) string {
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, projectDirMacro, projectRoot)
	if !filepath.IsAbs(value) {
		value = filepath.Join(projectRoot, value)
	}
	return filepath.Clean(value)
}

func group(folderName string) []string {
	if folderName == "" {
		return nil
	}
	return []string{folderName}
}

func envMap(envs *xmlEnvs) map[string]string {
	if envs == nil || len(envs.Env) == 0 {
		return nil
	}
	m := make(map[string]string, len(envs.Env))
	for _, e := range envs.Env {
		m[e.Name] = e.Value
	}
	return m
}

// splitArgs is a minimal whitespace tokenizer for JetBrains' single
// "parameters" string. It does not handle quoting/escaping - a
// reasonable first pass, not a full shell parser.
func splitArgs(s string) []string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func convert(c xmlConfigItem, projectRoot string) (RunConfig, bool) {
	if c.Name == "" {
		// A run configuration with no name can't become a service: it
		// has nothing to key dedup, selection, or supervisor lookups
		// on, and would otherwise show up as a blank, unselectable row.
		return RunConfig{}, false
	}
	switch c.Type {
	case "GoApplicationRunConfiguration":
		return convertGo(c, projectRoot)
	case "NodeJSConfigurationType":
		return convertNode(c, projectRoot)
	case "js.build_tools.npm":
		return convertNpm(c, projectRoot)
	case "ShConfigurationType":
		return convertShell(c, projectRoot)
	default:
		return RunConfig{}, false
	}
}

func convertGo(c xmlConfigItem, projectRoot string) (RunConfig, bool) {
	dir := valueOf(c.WorkingDirectory)
	if dir == "" {
		return RunConfig{}, false
	}
	return RunConfig{
		Name:      c.Name,
		IsGo:      true,
		Directory: resolvePath(projectRoot, dir),
		Args:      splitArgs(valueOf(c.Parameters)),
		Env:       envMap(c.Envs),
		Group:     group(c.FolderName),
	}, true
}

func convertNode(c xmlConfigItem, projectRoot string) (RunConfig, bool) {
	entry := valueOf(c.Path)
	if entry == "" {
		return RunConfig{}, false
	}
	dir := valueOf(c.WorkingDir)
	if dir == "" {
		dir = filepath.Dir(entry)
	}
	return RunConfig{
		Name:      c.Name,
		Directory: resolvePath(projectRoot, dir),
		Command:   append([]string{"node", resolvePath(projectRoot, entry)}, splitArgs(valueOf(c.Parameters))...),
		Env:       envMap(c.Envs),
		Group:     group(c.FolderName),
	}, true
}

func convertNpm(c xmlConfigItem, projectRoot string) (RunConfig, bool) {
	pkgJSON := valueOf(c.PackageJSON)
	if pkgJSON == "" || c.Scripts == nil || len(c.Scripts.Script) == 0 {
		return RunConfig{}, false
	}
	dir := filepath.Dir(resolvePath(projectRoot, pkgJSON))
	command := valueOf(c.Command)
	if command == "" {
		command = "run"
	}
	return RunConfig{
		Name:      c.Name,
		Directory: dir,
		Command:   []string{"npm", command, c.Scripts.Script[0].Value},
		Env:       envMap(c.Envs),
		Group:     group(c.FolderName),
	}, true
}

func convertShell(c xmlConfigItem, projectRoot string) (RunConfig, bool) {
	opts := make(map[string]string, len(c.Options))
	for _, o := range c.Options {
		opts[o.Name] = o.Value
	}
	script := opts["SCRIPT_PATH"]
	if script == "" {
		return RunConfig{}, false
	}
	interpreter := opts["INTERPRETER_PATH"]
	if interpreter == "" {
		interpreter = "/bin/sh"
	}
	dir := opts["SCRIPT_WORKING_DIRECTORY"]
	if dir == "" {
		dir = filepath.Dir(script)
	}
	command := []string{interpreter, resolvePath(projectRoot, script)}
	command = append(command, splitArgs(opts["SCRIPT_OPTIONS"])...)
	return RunConfig{
		Name:      c.Name,
		Directory: resolvePath(projectRoot, dir),
		Command:   command,
		Group:     group(c.FolderName),
	}, true
}

func valueOf(v *xmlValue) string {
	if v == nil {
		return ""
	}
	return v.Value
}
