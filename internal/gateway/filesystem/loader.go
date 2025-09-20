package filesystem

import "os"

// Loader abstracts file-system reads for use cases requiring SQL scripts.
type Loader interface {
	Load(path string) (string, error)
}

// FileLoader implements Loader by reading from the OS file system.
type FileLoader struct{}

// Load reads the file contents into a string.
func (FileLoader) Load(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
