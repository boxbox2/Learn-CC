package provider

import (
	"fmt"

	"mewcode/internal/config"
)

type Factory struct{}

type Constructor func(name string, cfg config.ProviderConfig) Provider

var constructors = map[string]Constructor{}

func Register(protocol string, constructor Constructor) {
	constructors[protocol] = constructor
}

func NewFactory() Factory {
	return Factory{}
}

func (Factory) Create(name string, cfg config.ProviderConfig) (Provider, error) {
	constructor, ok := constructors[cfg.Protocol]
	if !ok {
		return nil, fmt.Errorf("provider %q protocol %q is not supported", name, cfg.Protocol)
	}
	return constructor(name, cfg), nil
}
