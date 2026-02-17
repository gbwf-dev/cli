package manifest

type Base struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`

	Ref   string `yaml:"ref"`
	Color string `yaml:"color"`
}

type Manifest struct {
	Base    []Base `yaml:"base"`
	Plugins []Base `yaml:"plugins"`
}
