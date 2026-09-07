package shell

import (
	"io"
	"strings"
)

type consoleHistoryWriter struct {
	renderer *consoleRenderer
	pending  strings.Builder
}

func newConsoleHistoryWriter(renderer *consoleRenderer) *consoleHistoryWriter {
	return &consoleHistoryWriter{renderer: renderer}
}

func (writer *consoleHistoryWriter) Write(data []byte) (int, error) {
	if writer == nil || writer.renderer == nil {
		return 0, io.ErrClosedPipe
	}

	length := len(data)
	writer.pending.Write(data)

	for {
		value := writer.pending.String()
		index := strings.IndexByte(value, '\n')
		if index < 0 {
			break
		}

		line := value[:index]
		remainder := value[index+1:]

		writer.pending.Reset()
		writer.pending.WriteString(remainder)

		writer.renderer.AppendOutput(line)
	}

	return length, nil
}

func (writer *consoleHistoryWriter) Flush() {
	if writer == nil || writer.renderer == nil || writer.pending.Len() == 0 {
		return
	}

	writer.renderer.AppendOutput(writer.pending.String())
	writer.pending.Reset()
}
