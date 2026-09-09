package navmeshv2

import "bytes"

func splitMetadataLines(content []byte) [][]byte {
	lines := bytes.Split(content, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	for i := range lines {
		lines[i] = bytes.TrimSuffix(lines[i], []byte{'\r'})
	}
	return lines
}
