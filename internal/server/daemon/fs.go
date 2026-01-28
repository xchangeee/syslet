package daemon

import "os"

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0755)
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
