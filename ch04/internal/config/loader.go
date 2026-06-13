package config

import (
	"os"
	"path/filepath"

	"mewcode/internal/permission"

	"gopkg.in/yaml.v3"
)

type LoadOptions struct {
	HomeDir    string
	ProjectDir string
}

type LoadedConfig struct {
	Config           AppConfig
	PermissionLayers []permission.Layer
}

func LoadConfig(projectDir string) (AppConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return AppConfig{}, err
	}
	loaded, err := LoadDetailedWithOptions(LoadOptions{HomeDir: home, ProjectDir: projectDir})
	return loaded.Config, err
}

func LoadWithOptions(opts LoadOptions) (AppConfig, error) {
	loaded, err := LoadDetailedWithOptions(opts)
	return loaded.Config, err
}

func LoadDetailedWithOptions(opts LoadOptions) (LoadedConfig, error) {
	var merged AppConfig
	var layers []permission.Layer

	globalPath := filepath.Join(opts.HomeDir, ".mewcode", "config.yaml")
	global, err := readConfigIfExists(globalPath)
	if err != nil {
		return LoadedConfig{}, err
	}
	merged = mergeConfig(merged, global)
	if len(global.Permissions.Rules) > 0 {
		layers = append(layers, permission.Layer{Source: permission.Source{Kind: permission.SourceUser, Path: globalPath}, Rules: global.Permissions.Rules})
	}

	projectPath := filepath.Join(opts.ProjectDir, "mewcode.yaml")
	project, err := readConfigIfExists(projectPath)
	if err != nil {
		return LoadedConfig{}, err
	}
	merged = mergeConfig(merged, project)
	if len(project.Permissions.Rules) > 0 {
		layers = append(layers, permission.Layer{Source: permission.Source{Kind: permission.SourceProject, Path: projectPath}, Rules: project.Permissions.Rules})
	}

	localPath := filepath.Join(opts.ProjectDir, "mewcode.local.yaml")
	local, err := readConfigIfExists(localPath)
	if err != nil {
		return LoadedConfig{}, err
	}
	merged = mergeConfig(merged, local)
	if len(local.Permissions.Rules) > 0 {
		layers = append(layers, permission.Layer{Source: permission.Source{Kind: permission.SourceLocal, Path: localPath}, Rules: local.Permissions.Rules})
	}

	if err := Validate(merged); err != nil {
		return LoadedConfig{}, err
	}
	return LoadedConfig{Config: merged, PermissionLayers: layers}, nil
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
