package manifest

import "fmt"

type Validate interface {
	Validate() error
}

type Remote struct {
	Source string `yaml:"source"`
	Name   string `yaml:"name"`
	Ref    string `yaml:"ref"`
}

func (remote *Remote) Validate() error {
	if remote.Source == "" {
		return fmt.Errorf("remote.source cannot be empty")
	}
	return nil
}

type Base struct {
	Name  string `yaml:"name"`
	Color string `yaml:"color"`

	Remote Remote `yaml:"remote"`
}

func (base *Base) Validate() (err error) {
	err = base.Remote.Validate()
	return
}

type Manifest struct {
	Base    []Base `yaml:"base"`
	Plugins []Base `yaml:"plugins"`
}

func (manifest *Manifest) Validate() (err error) {
	if manifest.Base == nil {
		manifest.Base = make([]Base, 0)
	}
	if manifest.Plugins == nil {
		manifest.Plugins = make([]Base, 0)
	}

	for _, base := range manifest.Base {
		err = base.Validate()
		if err != nil {
			return
		}
	}
	for _, base := range manifest.Plugins {
		err = base.Validate()
		if err != nil {
			return
		}
	}
	return
}
