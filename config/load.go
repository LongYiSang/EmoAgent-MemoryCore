package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

type LoadOptions struct {
	SkipValidate bool
}

func LoadYAML(path string) (Config, error) {
	return LoadYAMLWithOptions(path, LoadOptions{})
}

func LoadYAMLWithOptions(path string, opts LoadOptions) (Config, error) {
	cfg, err := loadConfigFile(path, !opts.SkipValidate, decodeYAMLConfig)
	if err != nil {
		return Config{}, fmt.Errorf("load yaml config %s: %w", path, err)
	}
	return cfg, nil
}

func LoadJSON(path string) (Config, error) {
	return LoadJSONWithOptions(path, LoadOptions{})
}

func LoadJSONWithOptions(path string, opts LoadOptions) (Config, error) {
	cfg, err := loadConfigFile(path, !opts.SkipValidate, decodeJSONConfig)
	if err != nil {
		return Config{}, fmt.Errorf("load json config %s: %w", path, err)
	}
	return cfg, nil
}

func loadConfigFile(path string, validate bool, decode func([]byte, Config) (Config, error)) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return loadConfigBytes(data, validate, decode)
}

func loadConfigBytes(data []byte, validate bool, decode func([]byte, Config) (Config, error)) (Config, error) {
	cfg := Default()
	if len(bytes.TrimSpace(data)) == 0 {
		if validate {
			return cfg, cfg.Validate()
		}
		return cfg, nil
	}
	cfg, err := decode(data, cfg)
	if err != nil {
		if errors.Is(err, io.EOF) {
			if validate {
				return cfg, cfg.Validate()
			}
			return cfg, nil
		}
		return Config{}, err
	}
	if validate {
		if err := cfg.Validate(); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func decodeYAMLConfig(data []byte, cfg Config) (Config, error) {
	type plain Config
	decoded := plain(cfg)
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&decoded); err != nil {
		return Config{}, err
	}
	return Config(decoded), nil
}

func decodeJSONConfig(data []byte, cfg Config) (Config, error) {
	type plain Config
	decoded := plain(cfg)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return Config{}, err
	}
	return Config(decoded), nil
}
