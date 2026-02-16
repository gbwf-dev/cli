package manifest

type Base struct {
	Name  string `yaml:"name"`
	Color string `yaml:"color"`

	Source string `yaml:"source"`
}

type Plugin struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
}

type Manifest struct {
	Base    map[string]Base   `yaml:"base"`
	Plugins map[string]Plugin `yaml:"plugins"`
}
