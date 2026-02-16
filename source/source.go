package source

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type Source string

const (
	HTTP  Source = "http"
	HTTPS Source = "https"
	File  Source = "file"
)

type Driver struct {
	Raw    string
	Source Source
	Path   string
}

func Extract(raw string) (*Driver, error) {
	src, path, found := strings.Cut(raw, "://")
	if !found {
		return nil, fmt.Errorf("invalid source format. expected \"source://path\"")
	}

	source := Source(src)

	switch source {
	case HTTP, HTTPS, File:
		return &Driver{
			Raw:    raw,
			Source: source,
			Path:   path,
		}, nil
	default:
		return nil, fmt.Errorf("invalid source driver")
	}
}

func Resolve(source string) (reader io.ReadCloser, err error) {
	var driver *Driver
	driver, err = Extract(source)
	if err != nil {
		return
	}

	switch driver.Source {
	case HTTP, HTTPS:
		var response *http.Response
		response, err = http.Get(driver.Raw)
		if err != nil {
			return
		}
		reader = response.Body

	case File:
		reader, err = os.Open(driver.Path)
	}
	return
}
