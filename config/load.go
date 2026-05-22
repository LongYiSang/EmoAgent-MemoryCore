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
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := loadYAMLBytes(data, !opts.SkipValidate)
	if err != nil {
		return Config{}, fmt.Errorf("load yaml config %s: %w", path, err)
	}
	return cfg, nil
}

func LoadJSON(path string) (Config, error) {
	return LoadJSONWithOptions(path, LoadOptions{})
}

func LoadJSONWithOptions(path string, opts LoadOptions) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := loadJSONBytes(data, !opts.SkipValidate)
	if err != nil {
		return Config{}, fmt.Errorf("load json config %s: %w", path, err)
	}
	return cfg, nil
}

func loadYAMLBytes(data []byte, validate bool) (Config, error) {
	cfg := Default()
	if len(bytes.TrimSpace(data)) == 0 {
		if validate {
			return cfg, cfg.Validate()
		}
		return cfg, nil
	}
	var patch configPatch
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&patch); err != nil {
		if errors.Is(err, io.EOF) {
			return cfg, cfg.Validate()
		}
		return Config{}, err
	}
	applyConfigPatch(&cfg, patch)
	if validate {
		if err := cfg.Validate(); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func loadJSONBytes(data []byte, validate bool) (Config, error) {
	cfg := Default()
	if len(bytes.TrimSpace(data)) == 0 {
		if validate {
			return cfg, cfg.Validate()
		}
		return cfg, nil
	}
	var patch configPatch
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		if errors.Is(err, io.EOF) {
			if validate {
				return cfg, cfg.Validate()
			}
			return cfg, nil
		}
		return Config{}, err
	}
	applyConfigPatch(&cfg, patch)
	if validate {
		if err := cfg.Validate(); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}
