package logs

import (
	"bytes"
	"io"
)

type PrefixWriter struct {
	dest       io.Writer
	prefix     []byte
	needPrefix bool
}

func NewPrefixWriter(dest io.Writer, prefix string) *PrefixWriter {
	return &PrefixWriter{
		dest:       dest,
		prefix:     []byte(prefix),
		needPrefix: true,
	}
}

func (pw *PrefixWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	var buf bytes.Buffer

	for _, b := range p {
		if pw.needPrefix {
			buf.Write(pw.prefix)
			pw.needPrefix = false
		}
		buf.WriteByte(b)
		if b == '\n' {
			pw.needPrefix = true
		}
	}

	if _, err := pw.dest.Write(buf.Bytes()); err != nil {
		return 0, err
	}

	return len(p), nil
}
