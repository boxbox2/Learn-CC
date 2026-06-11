package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type LoadOptions struct {
	HomeDir    string
	ProjectDir string
}

func LoadConfig(projectDir string) (AppConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return AppConfig{}, err
	}
	return LoadWithOptions(LoadOptions{HomeDir: home, ProjectDir: projectDir})
}

func LoadWithOptions(opts LoadOptions) (AppConfig, error) {
	var merged AppConfig

	globalPath := filepath.Join(opts.HomeDir, ".mewcode", "config.yaml")
	global, err := readConfigIfExists(globalPath)
	if err != nil {
		return AppConfig{}, err
	}
	merged = mergeConfig(merged, global)

	projectPath := filepath.Join(opts.ProjectDir, "mewcode.yaml")
	project, err := readConfigIfExists(projectPath)
	if err != nil {
		return AppConfig{}, err
	}
	merged = mergeConfig(merged, project)

	if err := Validate(merged); err != nil {
		return AppConfig{}, err
	}
	return merged, nil
}

func readConfigIfExists(path string) (AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AppConfig{}, nil
		}
		return AppConfig{}, err
	}
	expanded := os.ExpandEnv(string(data))
	var cfg AppConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return AppConfig{}, err
	}
	return cfg, nil
}
