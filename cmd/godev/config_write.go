package main

import (
	"os"

	"github.com/abtinokhovat/godev/internal/config"
	"gopkg.in/yaml.v3"
)

func writeConfig(path string, f config.File) error {
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
